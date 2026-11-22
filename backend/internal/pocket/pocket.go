// Package pocket owns the Household's Pockets (PRD S10): listing them and
// creating one. Handlers live with their domain (docs/adr/go/0001).
//
// The dependency runs one way: this package imports auth, for the User in the
// request context and for the shared name rule, and auth never imports this
// one.
package pocket

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
)

// maxPocketDisplayNameCodePoints caps a Pocket's name, in code points, on the
// trimmed name. The same 60 as a Household's; the contract's maxLength of 255 is
// an outer bound that counts padding.
const maxPocketDisplayNameCodePoints = 60

// nameIndex is the partial unique index on a Household's live Pocket names. A
// 23505 on it is NAME_TAKEN; it is matched by this name, never by the SQLSTATE
// alone, because the primary key can raise 23505 too.
const nameIndex = "pockets_household_name_idx"

// Handlers implements the Pocket half of api.StrictServerInterface.
type Handlers struct {
	q db.Querier
}

type Deps struct {
	Querier db.Querier
}

func New(d Deps) *Handlers {
	return &Handlers{q: d.Querier}
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
	name, rule := auth.CheckBoundedName(body.PocketDisplayName, maxPocketDisplayNameCodePoints)
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
