// Package pocket owns the Household's Pockets (PRD S10): listing them,
// creating one, renaming, deactivating or reactivating one, and deleting and
// restoring one. Handlers live with their domain (docs/adr/go/0001).
//
// The dependency runs one way: this package imports auth, for the User in the
// request context and for the shared name rule, and auth never imports this
// one.
package pocket

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
)

// nameIndex is the partial unique index on a Household's live Pocket names. A
// 23505 on it is NAME_TAKEN; it is matched by this name, never by the SQLSTATE
// alone, because the primary key can raise 23505 too.
const nameIndex = "pockets_household_name_idx"

// Handlers implements the Pocket half of api.StrictServerInterface.
type Handlers struct {
	q  db.Querier
	tx auth.Beginner
}

// Deps. Beginner is for the two writers that can take a Pocket out of the
// active set (DeletePocket, and UpdatePocket with active = false), which run in
// a transaction under the Household's row lock.
type Deps struct {
	Querier  db.Querier
	Beginner auth.Beginner
}

func New(d Deps) *Handlers {
	return &Handlers{q: d.Querier, tx: d.Beginner}
}

// ListPockets answers every Pocket of the caller's Household that is not
// deleted, inactive ones included, in creation order. The Household is the
// authenticated User's, taken from the context and never from the request, and
// the SELECT filters on it in SQL.
func (h *Handlers) ListPockets(ctx context.Context, _ api.ListPocketsRequestObject) (api.ListPocketsResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		// Unreachable through the router: httpserver's sessionRequired answers
		// 401 for this operation first. Kept so a router change that lost the
		// check degrades to the contract's 401, not a SELECT with a zero
		// household_id.
		return api.ListPockets401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}

	rows, err := h.q.ListPockets(ctx, user.HouseholdID)
	if err != nil {
		slog.Error("list pockets", "err", err)
		return api.ListPockets500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}
	items := make([]api.Pocket, 0, len(rows))
	for _, row := range rows {
		items = append(items, response(row))
	}
	return api.ListPockets200JSONResponse{Items: items}, nil
}

// CreatePocket creates a Pocket, idempotently on the client's id (BOOTSTRAP.md
// §4). One INSERT ... ON CONFLICT (id) DO NOTHING: a row back is a create
// (201). No row means the id is held, and the read-back decides: the caller's
// own live Pocket is a replay (200, the Pocket as stored, the rest of the body
// neither compared nor applied); anything else, another Household's or the
// caller's own deleted one, is ID_TAKEN. Only pgx.ErrNoRows means "the id is
// held"; apart from the name clash, any other error is a 500 with nothing written.
//
// There is no transaction and no detached context: it is one statement, and
// retrying with the same id is the recovery for a client that went away.
func (h *Handlers) CreatePocket(ctx context.Context, request api.CreatePocketRequestObject) (api.CreatePocketResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return api.CreatePocket401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}
	body := request.Body
	if body == nil {
		// Unreachable: the contract's body is required, and the validator
		// refuses an absent one first.
		return api.CreatePocket400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{Code: api.INVALIDJSONBODY},
		}, nil
	}

	// A body that is invalid is refused whatever the id holds: this runs before
	// the database is asked anything, so a taken id with a bad name answers what
	// a free id with a bad name does.
	name, rule := auth.CheckPocketDisplayName(body.PocketDisplayName)
	if rule != "" {
		return api.CreatePocket400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{
				Code: api.VALIDATION,
				Args: &map[string]interface{}{"field": "pocket_display_name", "rule": rule},
			},
		}, nil
	}

	id := pgtype.UUID{Bytes: body.Id, Valid: true}
	row, err := h.q.CreatePocket(ctx, db.CreatePocketParams{
		ID:                id,
		HouseholdID:       user.HouseholdID,
		PocketDisplayName: name,
	})
	switch {
	case err == nil:
		return api.CreatePocket201JSONResponse(response(row)), nil
	case errors.Is(err, pgx.ErrNoRows):
		// The id is held.
		return h.replayOr(ctx, user, id, api.IDTAKEN), nil
	case isNameTaken(err):
		// Usually a different Pocket holds the name. But a concurrent replay of
		// this very id can raise it too: ON CONFLICT (id) arbitrates the primary
		// key only, and when the first insert has not committed yet the second
		// passes the arbiter's pre-check, inserts its tuple and then trips over
		// the first's entry in the name index, which is not an arbiter. The first
		// has committed by the time the error is raised, so the read-back tells
		// the two apart: the id now names a live Pocket of this Household, and
		// this was a replay (200), as it would have been a moment later.
		return h.replayOr(ctx, user, id, api.NAMETAKEN), nil
	default:
		// Never the request's body: a Pocket's name is user content.
		slog.Error("create pocket", "err", err)
		return createInternalError(), nil
	}
}

// replayOr is the read-back of a create that did not insert: the caller's own
// live Pocket under the id is a replay (200, as stored); no row is the given
// conflict; any other error is a 500, never a conflict.
func (h *Handlers) replayOr(ctx context.Context, user db.User, id pgtype.UUID, conflictCode api.ErrorCode) api.CreatePocketResponseObject {
	row, err := h.q.GetLivePocket(ctx, db.GetLivePocketParams{ID: id, HouseholdID: user.HouseholdID})
	switch {
	case err == nil:
		return api.CreatePocket200JSONResponse(response(row))
	case errors.Is(err, pgx.ErrNoRows):
		return conflict(conflictCode)
	default:
		slog.Error("create pocket: read back", "err", err)
		return createInternalError()
	}
}

// UpdatePocket renames, deactivates or reactivates a Pocket. Merge semantics,
// one statement, last write wins per field (BOOTSTRAP.md §4): a field left out
// is not touched, and two writers of different fields both land. Deactivating
// keeps the name and the Pocket's place in the list; it is not a delete.
//
// The Household is the authenticated User's, taken from the context and never
// from the request, and the UPDATE filters on it in SQL. An id that is unknown,
// another Household's or deleted is no row and answers one NOT_FOUND, `{}`
// included: nothing here confirms another Household's id. The contract's rules
// and the name rule run before the statement, so a foreign id with a bad body
// answers what an unknown one does.
//
// A request that leaves the Pocket active (active absent or true) is that one
// statement, and the answer is the row the UPDATE returns. `active: false` is
// the other half of the invariant that a Household always keeps one active
// Pocket (#140 R1), so it runs the guarded transaction of removeFromActive: it is
// refused with 409 LAST_POCKET, and nothing in the request applied, when this is
// the Household's last active Pocket. NOT_FOUND is decided first, so LAST_POCKET
// is only ever said about a Pocket of the caller's own Household.
func (h *Handlers) UpdatePocket(ctx context.Context, request api.UpdatePocketRequestObject) (api.UpdatePocketResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		// Unreachable through the router: httpserver's sessionRequired answers
		// 401 for this operation first. Kept so a router change that lost the
		// check degrades to the contract's 401, not an UPDATE with a zero
		// household_id.
		return api.UpdatePocket401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}
	body := request.Body
	if body == nil {
		// Unreachable: the contract's body is required, and the validator
		// refuses an absent one first.
		return api.UpdatePocket400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{Code: api.INVALIDJSONBODY},
		}, nil
	}

	params := db.UpdatePocketParams{
		ID:          pgtype.UUID{Bytes: request.Id, Valid: true},
		HouseholdID: user.HouseholdID,
		Active:      body.Active,
	}
	if body.PocketDisplayName != nil {
		name, rule := auth.CheckPocketDisplayName(*body.PocketDisplayName)
		if rule != "" {
			return api.UpdatePocket400JSONResponse{
				BadRequestJSONResponse: api.BadRequestJSONResponse{
					Code: api.VALIDATION,
					Args: &map[string]interface{}{"field": "pocket_display_name", "rule": rule},
				},
			}, nil
		}
		params.PocketDisplayName = &name
	}

	var row db.Pocket
	var err error
	if body.Active != nil && !*body.Active {
		err = h.removeFromActive(ctx, user, params.ID, func(q *db.Queries) error {
			var uerr error
			row, uerr = q.UpdatePocket(ctx, params)
			switch {
			case errors.Is(uerr, pgx.ErrNoRows):
				// Locked a statement ago, so this is not a race, but the
				// answer for no row is the same either way.
				return errNotFound
			case isNameTaken(uerr):
				return errNameTaken
			case uerr != nil:
				return opaque("update", uerr)
			}
			return nil
		}, false)
	} else {
		row, err = h.q.UpdatePocket(ctx, params)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			err = errNotFound
		case isNameTaken(err):
			err = errNameTaken
		}
	}
	switch {
	case err == nil:
		return api.UpdatePocket200JSONResponse(response(row)), nil
	case errors.Is(err, errNotFound):
		return api.UpdatePocket404JSONResponse{
			NotFoundJSONResponse: api.NotFoundJSONResponse{Code: api.NOTFOUND},
		}, nil
	case errors.Is(err, errNameTaken):
		return api.UpdatePocket409JSONResponse{
			ConflictJSONResponse: api.ConflictJSONResponse{Code: api.NAMETAKEN},
		}, nil
	case errors.Is(err, errLastPocket):
		return api.UpdatePocket409JSONResponse{
			ConflictJSONResponse: api.ConflictJSONResponse{Code: api.LASTPOCKET},
		}, nil
	default:
		// Never the request's body: a Pocket's name is user content.
		slog.Error("update pocket", "err", err)
		return api.UpdatePocket500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}
}

// DeletePocket soft-deletes a Pocket (BOOTSTRAP.md §4): the row stays with
// deleted_at set, and the Pocket is gone from the list, from PATCH, and from
// the name index, which frees its name. Idempotent on the caller's own Pocket
// and keeping the first deletion instant, so deleting a deleted one is 204
// again and is never refused. An id that is unknown or another Household's is
// the one NOT_FOUND. The Household is the authenticated User's, taken from the
// context and never from the request, and every statement filters on it in SQL.
//
// LAST_POCKET (#140 R1): a Household always keeps one active Pocket, so deleting
// its last active one is refused, 409, with nothing changed. Deleting an
// inactive Pocket is never refused for this, since it leaves the active set as
// it was. NOT_FOUND is decided before LAST_POCKET, so the refusal is only ever
// said about a Pocket of the caller's own Household, and a foreign id answers
// what an unknown one does. See removeFromActive for the transaction.
//
// The in-use guard (PRD R1: 409 POCKET_IN_USE while any Expense references the
// Pocket) arrives with the Expenses table and joins this transaction after the
// same Household lock; see queries/pocket.sql. There is no Expense table yet,
// so there is nothing to refuse and no code for it in the contract.
func (h *Handlers) DeletePocket(ctx context.Context, request api.DeletePocketRequestObject) (api.DeletePocketResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		// Unreachable through the router: httpserver's sessionRequired answers
		// 401 for this operation first. Kept so a router change that lost the
		// check degrades to the contract's 401, not an UPDATE with a zero
		// household_id.
		return api.DeletePocket401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}

	id := pgtype.UUID{Bytes: request.Id, Valid: true}
	err := h.removeFromActive(ctx, user, id, func(q *db.Queries) error {
		n, err := q.SoftDeletePocket(ctx, db.SoftDeletePocketParams{ID: id, HouseholdID: user.HouseholdID})
		if err != nil {
			return opaque("delete", err)
		}
		if n == 0 {
			// The row was locked a statement ago, so this is not a race: a
			// statement that disagrees with its own lock is a failure.
			return errors.New("delete: the locked row was not updated")
		}
		return nil
	}, true)
	switch {
	case err == nil:
		return api.DeletePocket204Response{}, nil
	case errors.Is(err, errLastPocket):
		return api.DeletePocket409JSONResponse{
			ConflictJSONResponse: api.ConflictJSONResponse{Code: api.LASTPOCKET},
		}, nil
	case errors.Is(err, errNotFound):
		return api.DeletePocket404JSONResponse{
			NotFoundJSONResponse: api.NotFoundJSONResponse{Code: api.NOTFOUND},
		}, nil
	default:
		slog.Error("delete pocket", "err", err)
		return api.DeletePocket500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}
}

// removeFromActive runs write, which takes the Pocket out of the active set, in
// the one transaction that keeps "a Household always has an active Pocket"
// true. Statements, in this order:
//
//  1. LockHousehold: SELECT ... FOR UPDATE on the Household's row. Every writer
//     that can shrink the set takes it first, so two of them are ordered here
//     and the second waits.
//  2. LockPocket: the Pocket's own row, which says whether it is the caller's
//     (no row is NOT_FOUND), deleted already, and active.
//  3. OtherActivePocketExists, a SEPARATE statement: its snapshot is taken only
//     after the lock in 1 was granted, so it sees everything the writer ahead of
//     it committed. Folded into the write's WHERE it would have been evaluated
//     before the wait ended, and two removals of the last two Pockets would each
//     count the other still there (BOOTSTRAP.md §4; proven by the race tests).
//  4. write.
//
// A Pocket that is already deleted is the idempotent success of a delete (the
// guard does not apply to what is no longer there, and nothing is written:
// idempotentWhenDeleted) and NOT_FOUND to a PATCH. An inactive Pocket skips step 3: taking it out of the active set changes
// nothing about the count. Nothing is written before step 4, so a refusal
// leaves no trace.
//
// Restore and create only add Pockets to the set, so they take no lock: a count
// that misses one that has not committed is conservative, and the answer is the
// one a request ordered ahead of it would have got.
func (h *Handlers) removeFromActive(ctx context.Context, user db.User, id pgtype.UUID, write func(q *db.Queries) error, idempotentWhenDeleted bool) error {
	return h.inTx(ctx, func(q *db.Queries) error {
		if _, err := q.LockHousehold(ctx, user.HouseholdID); err != nil {
			return opaque("lock the household", err)
		}
		state, err := q.LockPocket(ctx, db.LockPocketParams{ID: id, HouseholdID: user.HouseholdID})
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return opaque("lock the pocket", err)
		}
		if state.DeletedAt.Valid {
			if idempotentWhenDeleted {
				return nil
			}
			return errNotFound
		}
		if state.Active {
			others, err := q.OtherActivePocketExists(ctx, db.OtherActivePocketExistsParams{
				HouseholdID: user.HouseholdID,
				ID:          id,
			})
			if err != nil {
				return opaque("count the other active pockets", err)
			}
			if !others {
				return errLastPocket
			}
		}
		return write(q)
	})
}

var (
	// errLastPocket is the guard's refusal, carried out of the transaction so
	// it rolls back.
	errLastPocket = errors.New("the Pocket is the Household's last active one")
	// errNotFound is no live Pocket of the caller's Household under the id.
	errNotFound = errors.New("no such Pocket in the Household")
	// errNameTaken is a rename into a name another live Pocket holds.
	errNameTaken = errors.New("the name is held by another Pocket")
)

// opaque keeps an error's text and drops its chain, so a 23505 or ErrNoRows
// from a later statement is never read as the one the INSERT or UPDATE raised.
func opaque(step string, err error) error {
	return fmt.Errorf("%s: %v", step, err)
}

// inTx runs fn in a transaction, committed only if fn returns nil. An error
// comes back as fn returned it.
func (h *Handlers) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := h.tx.Begin(ctx)
	if err != nil {
		return opaque("begin", err)
	}
	// A no-op after Commit. Every other return rolls back. Detached from ctx so
	// a cancelled request still releases the connection.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := fn(db.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return opaque("commit", err)
	}
	return nil
}

// RestorePocket is the Undo of DeletePocket: it clears deleted_at and answers
// the Pocket, active as it was. One statement, idempotent (a live Pocket is
// 200, unchanged). An id that is unknown or another Household's is the one
// NOT_FOUND. A name that another live Pocket of the Household now holds is a
// 23505 on the name index, which is NAME_TAKEN with the Pocket still deleted:
// the index decides it, so two concurrent restores into one name answer one 200
// and one 409, never a 500.
func (h *Handlers) RestorePocket(ctx context.Context, request api.RestorePocketRequestObject) (api.RestorePocketResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return api.RestorePocket401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}

	row, err := h.q.RestorePocket(ctx, db.RestorePocketParams{
		ID:          pgtype.UUID{Bytes: request.Id, Valid: true},
		HouseholdID: user.HouseholdID,
	})
	switch {
	case err == nil:
		return api.RestorePocket200JSONResponse(response(row)), nil
	case errors.Is(err, pgx.ErrNoRows):
		return api.RestorePocket404JSONResponse{
			NotFoundJSONResponse: api.NotFoundJSONResponse{Code: api.NOTFOUND},
		}, nil
	case isNameTaken(err):
		return api.RestorePocket409JSONResponse{
			ConflictJSONResponse: api.ConflictJSONResponse{Code: api.NAMETAKEN},
		}, nil
	default:
		slog.Error("restore pocket", "err", err)
		return api.RestorePocket500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}
}

func response(row db.Pocket) api.Pocket {
	return api.Pocket{
		Id:                openapi_types.UUID(row.ID.Bytes),
		PocketDisplayName: row.PocketDisplayName,
		Active:            row.Active,
	}
}

func isNameTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == nameIndex
}

func conflict(code api.ErrorCode) api.CreatePocketResponseObject {
	return api.CreatePocket409JSONResponse{ConflictJSONResponse: api.ConflictJSONResponse{Code: code}}
}

// createInternalError is this package's own 500: auth's panics for a response
// type it does not list, and these are not on it.
func createInternalError() api.CreatePocketResponseObject {
	return api.CreatePocket500JSONResponse{
		InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
	}
}
