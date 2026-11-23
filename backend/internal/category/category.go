// Package category owns the Household's Categories (PRD S8): listing them,
// creating one with the allocation it starts with, and renaming, deactivating
// or reactivating one. What a Category is planned at afterwards is the Budget's
// (budget.UpdateBudget). Handlers live with their
// domain (docs/adr/go/0001).
//
// The dependency runs one way: this package imports auth, for the User in the
// request context and the shared name rule, and budget, to write the first
// allocation inside its own transaction. Neither imports this one: budget reads
// Categories through sqlc.
package category

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/db"
)

// maxCategoryDisplayNameCodePoints caps a Category's name, in code points, on
// the trimmed name: the same 60 as a Pocket's and a Household's.
const maxCategoryDisplayNameCodePoints = 60

// nameIndex is the partial unique index on a Household's live Category names,
// across both buckets. A 23505 on it is NAME_TAKEN; it is matched by this name,
// never by the SQLSTATE alone, because the primary key can raise 23505 too.
const nameIndex = "categories_household_name_idx"

// householdIDKey is the unique constraint on (household_id, id), which the
// composite foreign key from allocations needs. It is a second unique index on
// the id, and ON CONFLICT (id) does not arbitrate it: see isIDKeyClash.
const householdIDKey = "categories_household_id_id_key"

// Handlers implements the Category half of api.StrictServerInterface.
type Handlers struct {
	q   db.Querier
	tx  budget.Beginner
	now func() time.Time
}

type Deps struct {
	Querier  db.Querier
	Beginner budget.Beginner
	// Now is the clock the first allocation's Period Day is worked out from.
	// Production passes time.Now. Nil means time.Now.
	Now func() time.Time
}

func New(d Deps) *Handlers {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Handlers{q: d.Querier, tx: d.Beginner, now: now}
}

// ListCategories answers every Category of the caller's Household that is not
// deleted, inactive ones included, the two buckets together, in creation order.
// The Household is the authenticated User's, taken from the context and never
// from the request, and the SELECT filters on it in SQL.
func (h *Handlers) ListCategories(ctx context.Context, _ api.ListCategoriesRequestObject) (api.ListCategoriesResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		// Unreachable through the router: httpserver's sessionRequired answers
		// 401 for this operation first. Kept so a router change that lost the
		// check degrades to the contract's 401, not a SELECT with a zero
		// household_id.
		return api.ListCategories401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}

	rows, err := h.q.ListCategories(ctx, user.HouseholdID)
	if err != nil {
		slog.Error("list categories", "err", err)
		return api.ListCategories500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}
	items := make([]api.Category, 0, len(rows))
	for _, row := range rows {
		items = append(items, response(row))
	}
	return api.ListCategories200JSONResponse{Items: items}, nil
}

// CreateCategory creates a Category, idempotently on the client's id
// (BOOTSTRAP.md §4), as CreatePocket does: one INSERT ... ON CONFLICT (id) DO
// NOTHING, a row back is a create (201), no row means the id is held and the
// read-back decides (the caller's own live Category is a replay, 200, as stored,
// the rest of the body neither compared nor applied; anything else is ID_TAKEN).
//
// The order of checks is the contract's, then the name, then the Commitment's
// allocation (PRD S8: required, `null` is as absent, "0" is an allocation), then
// the database. A body that is invalid is refused whatever the id holds.
//
// With an allocation the Category and the allocation are one transaction, with
// the Period it lands in materialised in it (PRD S13): a failure at any step
// leaves neither. It runs on the request's context, so a client that goes away
// rolls it back and retrying with the same id is the recovery. A replay applies
// no allocation: the transaction that finds the id held writes nothing.
func (h *Handlers) CreateCategory(ctx context.Context, request api.CreateCategoryRequestObject) (api.CreateCategoryResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return api.CreateCategory401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}
	body := request.Body
	if body == nil || !body.Bucket.Valid() {
		// Unreachable: the contract's body is required and bucket is an enum, and
		// the validator refuses both first.
		return api.CreateCategory400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{Code: api.INVALIDJSONBODY},
		}, nil
	}

	name, rule := auth.CheckBoundedName(body.CategoryDisplayName, maxCategoryDisplayNameCodePoints)
	if rule != "" {
		return validation("category_display_name", rule), nil
	}

	var allocation *decimal.Decimal
	if body.Allocation != nil {
		d, err := decimal.NewFromString(*body.Allocation)
		if err != nil {
			// Unreachable: the contract's pattern admits only what parses.
			return validation("allocation", "pattern"), nil
		}
		allocation = &d
	}
	if body.Bucket == api.Commitment && allocation == nil {
		return validation("allocation", "required"), nil
	}

	id := pgtype.UUID{Bytes: body.Id, Valid: true}
	params := db.CreateCategoryParams{
		ID:                  id,
		HouseholdID:         user.HouseholdID,
		CategoryDisplayName: name,
		Bucket:              string(body.Bucket),
	}

	var row db.Category
	var err error
	if allocation == nil {
		row, err = h.q.CreateCategory(ctx, params)
	} else {
		row, err = h.createWithAllocation(ctx, user, params, *allocation)
	}
	switch {
	case err == nil:
		return api.CreateCategory201JSONResponse(response(row)), nil
	case errors.Is(err, pgx.ErrNoRows):
		// The id is held.
		return h.replayOr(ctx, user, id, api.IDTAKEN), nil
	case isNameTaken(err):
		// Usually a different Category holds the name. But a concurrent replay
		// of this very id can raise it too (see CreatePocket): the first insert
		// has committed by the time the error is raised, so the read-back tells
		// the two apart.
		return h.replayOr(ctx, user, id, api.NAMETAKEN), nil
	case isIDKeyClash(err):
		// Only a concurrent replay of this very id: see isIDKeyClash. The first
		// insert has committed by the time the error is raised, so the
		// read-back finds its Category (200), or finds nothing because it was
		// deleted in the meantime (an id names one Category for ever).
		return h.replayOr(ctx, user, id, api.IDTAKEN), nil
	default:
		// Never the request's body: a Category's name is user content, and an
		// allocation is an amount.
		slog.Error("create category", "err", err)
		return createInternalError(), nil
	}
}

// UpdateCategory renames, deactivates or reactivates a Category. Merge
// semantics, one statement, last write wins per field (BOOTSTRAP.md §4), as
// UpdatePocket: a field left out is not touched, and two writers of different
// fields both land. Deactivating keeps the name, the place in the list and the
// Category's allocation, so no figure moves; it is not a delete. The bucket is
// not a property of the request, so it cannot be changed here (PRD S8, #130 C7).
//
// The Household is the authenticated User's, taken from the context and never
// from the request, and the UPDATE filters on it in SQL. An id that is unknown,
// another Household's or deleted is no row and answers one NOT_FOUND, `{}`
// included: nothing here confirms another Household's id. The contract's rules
// and the name rule run before the statement, so a foreign id with a bad body
// answers what an unknown one does. No transaction: the answer is the row the
// UPDATE returns.
func (h *Handlers) UpdateCategory(ctx context.Context, request api.UpdateCategoryRequestObject) (api.UpdateCategoryResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		// Unreachable through the router: httpserver's sessionRequired answers
		// 401 for this operation first. Kept so a router change that lost the
		// check degrades to the contract's 401, not an UPDATE with a zero
		// household_id.
		return api.UpdateCategory401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}
	body := request.Body
	if body == nil {
		// Unreachable: the contract's body is required, and the validator
		// refuses an absent one first.
		return api.UpdateCategory400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{Code: api.INVALIDJSONBODY},
		}, nil
	}

	params := db.UpdateCategoryParams{
		ID:          pgtype.UUID{Bytes: request.Id, Valid: true},
		HouseholdID: user.HouseholdID,
		Active:      body.Active,
	}
	if body.CategoryDisplayName != nil {
		name, rule := auth.CheckBoundedName(*body.CategoryDisplayName, maxCategoryDisplayNameCodePoints)
		if rule != "" {
			return api.UpdateCategory400JSONResponse{
				BadRequestJSONResponse: api.BadRequestJSONResponse{
					Code: api.VALIDATION,
					Args: &map[string]interface{}{"field": "category_display_name", "rule": rule},
				},
			}, nil
		}
		params.CategoryDisplayName = &name
	}

	row, err := h.q.UpdateCategory(ctx, params)
	switch {
	case err == nil:
		return api.UpdateCategory200JSONResponse(response(row)), nil
	case errors.Is(err, pgx.ErrNoRows):
		return api.UpdateCategory404JSONResponse{
			NotFoundJSONResponse: api.NotFoundJSONResponse{Code: api.NOTFOUND},
		}, nil
	case isNameTaken(err):
		return api.UpdateCategory409JSONResponse{
			ConflictJSONResponse: api.ConflictJSONResponse{Code: api.NAMETAKEN},
		}, nil
	default:
		// Never the request's body: a Category's name is user content.
		slog.Error("update category", "err", err)
		return api.UpdateCategory500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}
}

// createWithAllocation inserts the Category, brings the current Period into
// being if this is the first request to find it missing, and records the
// allocation dated the writer's Today, in one transaction. The error is the
// insert's own when that is what failed (pgx.ErrNoRows for a held id, a 23505
// for a name), so CreateCategory classifies it as it does the single statement's.
func (h *Handlers) createWithAllocation(ctx context.Context, user db.User, params db.CreateCategoryParams, allocation decimal.Decimal) (db.Category, error) {
	tx, err := h.tx.Begin(ctx)
	if err != nil {
		return db.Category{}, opaque("begin", err)
	}
	// A no-op after Commit. Every other return rolls back. Detached from ctx so
	// a cancelled request still releases the connection.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := db.New(tx)

	row, err := q.CreateCategory(ctx, params)
	if err != nil {
		return db.Category{}, err
	}
	at, err := budget.Locate(ctx, q, h.now, user)
	if err != nil {
		return db.Category{}, opaque("resolve the period", err)
	}
	if err := budget.Materialise(ctx, q, user.HouseholdID, at.Period); err != nil {
		return db.Category{}, opaque("materialise the period", err)
	}
	if err := budget.SetAllocation(ctx, q, user.HouseholdID, at, row.ID, allocation); err != nil {
		return db.Category{}, opaque("set the allocation", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Category{}, opaque("commit", err)
	}
	return row, nil
}

// opaque keeps an error's text and drops its chain. CreateCategory reads
// pgx.ErrNoRows as "the id is held" and a 23505 on the name index as a name
// clash, and only the INSERT's own error may mean either: the same error from a
// later step is a failure, a 500.
func opaque(step string, err error) error {
	return fmt.Errorf("%s: %v", step, err)
}

// replayOr is the read-back of a create that did not insert: the caller's own
// live Category under the id is a replay (200, as stored); no row is the given
// conflict; any other error is a 500, never a conflict.
func (h *Handlers) replayOr(ctx context.Context, user db.User, id pgtype.UUID, conflictCode api.ErrorCode) api.CreateCategoryResponseObject {
	row, err := h.q.GetLiveCategory(ctx, db.GetLiveCategoryParams{ID: id, HouseholdID: user.HouseholdID})
	switch {
	case err == nil:
		return api.CreateCategory200JSONResponse(response(row))
	case errors.Is(err, pgx.ErrNoRows):
		return api.CreateCategory409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse{Code: conflictCode}}
	default:
		slog.Error("create category: read back", "err", err)
		return createInternalError()
	}
}

func response(row db.Category) api.Category {
	return api.Category{
		Id:                  openapi_types.UUID(row.ID.Bytes),
		CategoryDisplayName: row.CategoryDisplayName,
		Bucket:              api.Bucket(row.Bucket),
		Active:              row.Active,
	}
}

func isNameTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == nameIndex
}

// isIDKeyClash is the same race as a name clash, on the other index that is not
// the arbiter. A Category's id is unique twice (the primary key, and
// (household_id, id) for the allocations' foreign key), and ON CONFLICT (id)
// arbitrates only the first: a concurrent replay of one id passes the arbiter's
// pre-check while the first insert is uncommitted, inserts its tuple, and trips
// over the first's entry in the second index once it commits. Left alone it is
// a 500 for a request that should have been a 200.
func isIDKeyClash(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == householdIDKey
}

func validation(field, rule string) api.CreateCategoryResponseObject {
	return api.CreateCategory400JSONResponse{
		BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: api.VALIDATION,
			Args: &map[string]interface{}{"field": field, "rule": rule},
		},
	}
}

// createInternalError is this package's own 500: auth's panics for a response
// type it does not list, and these are not on it.
func createInternalError() api.CreateCategoryResponseObject {
	return api.CreateCategory500JSONResponse{
		InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
	}
}
