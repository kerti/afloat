// Package expense owns the Household's Expenses (PRD E1 to E10): recording one
// and listing them. Correcting, deleting and restoring one are later slices of
// #140. Handlers live with their domain (docs/adr/go/0001).
//
// The dependency runs one way: this package imports auth, for the User in the
// request context and the shared text rule, and budget, for the one place a
// request's Period Day is derived. Neither imports this one: the guards that
// read Expenses (a Pocket's delete, a Category's, the currency change) go
// through sqlc.
package expense

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
	"github.com/kerti/afloat/backend/internal/period"
)

// maxDescriptionCodePoints caps an Expense's description, in code points, on the
// trimmed text (#140 R9). The contract's maxLength of 1000 is an outer bound
// that counts padding.
const maxDescriptionCodePoints = 200

// Handlers implements the Expense half of api.StrictServerInterface.
type Handlers struct {
	q   db.Querier
	tx  auth.Beginner
	now func() time.Time
}

// Deps. Beginner is for the create, which is one transaction. Now is the clock
// an absent occurred_on is resolved from; production passes time.Now and nil
// means time.Now.
type Deps struct {
	Querier  db.Querier
	Beginner auth.Beginner
	Now      func() time.Time
}

func New(d Deps) *Handlers {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Handlers{q: d.Querier, tx: d.Beginner, now: now}
}

// CreateExpense records an Expense, idempotently on the client's id
// (BOOTSTRAP.md §4, #140 X4). Unlike a Pocket's, its create has references whose
// state can change (the Pocket is active, the Category is live, the Household's
// currency), so an `INSERT ... SELECT` that checked them would make
// pgx.ErrNoRows mean two things, "the id is held" and "a reference failed". It
// is one transaction of statements in this order instead:
//
//  1. Read back by id. The caller's own live Expense is a replay, 200, as it
//     stands, and nothing else is looked at: a retry after the Pocket was
//     deactivated is still a replay, never a refusal.
//  2. Lock, FOR SHARE and in this order, the Household (which also says the
//     currency to stamp), then the Pocket, then the Category and the owner when
//     the body names them, each with its liveness predicate in the locking
//     statement. The order is the one every removal takes (LAST_POCKET's, the
//     guards'), so nothing waits on a row another holds while holding one it
//     wants. A reference that is not live, or not the caller's, is one answer
//     (`invalid`); the caller's own inactive Pocket or Category is `inactive`.
//  3. INSERT ... ON CONFLICT (id) DO NOTHING RETURNING. Only here does
//     pgx.ErrNoRows mean "the id is held": a concurrent replay that has
//     committed meanwhile. It is read back, 200 or ID_TAKEN.
//
// The shared locks are what the guards in the other direction rely on: a delete
// that locked the Pocket first is waited for, and a create that came first is
// seen by the delete's EXISTS.
//
// The body's own rules (the amount's zero, the date, the description, the
// ownership pair) are judged before the database is asked anything, so a taken
// id with a bad body answers what a free id with a bad body does, and the least
// (field, rule) is the one reported. Nothing here logs a description or an
// amount.
func (h *Handlers) CreateExpense(ctx context.Context, request api.CreateExpenseRequestObject) (api.CreateExpenseResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return api.CreateExpense401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}
	body := request.Body
	if body == nil || (body.Ownership != nil && !body.Ownership.Valid()) {
		// Unreachable: the contract's body is required and ownership is an enum,
		// and the validator refuses both first.
		return api.CreateExpense400JSONResponse{
			BadRequestJSONResponse: api.BadRequestJSONResponse{Code: api.INVALIDJSONBODY},
		}, nil
	}

	in, bad := checkBody(body)
	if bad != nil {
		return validation(*bad), nil
	}

	var row db.Expense
	var replay bool
	err := h.inTx(ctx, func(q *db.Queries) error {
		id := pgtype.UUID{Bytes: body.Id, Valid: true}
		existing, err := q.GetLiveExpense(ctx, db.GetLiveExpenseParams{ID: id, HouseholdID: user.HouseholdID})
		switch {
		case err == nil:
			row, replay = existing, true
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return opaque("read back", err)
		}

		currency, err := q.LockHouseholdShared(ctx, user.HouseholdID)
		if err != nil {
			// No row is a Household that is gone, which a live session cannot
			// have; either way it is not the request's fault.
			return opaque("lock the household", err)
		}
		var refused []failure
		if f, err := lockReference("pocket_id", func() (bool, error) {
			return q.LockLivePocketShared(ctx, db.LockLivePocketSharedParams{ID: in.pocket, HouseholdID: user.HouseholdID})
		}); err != nil {
			return err
		} else if f != nil {
			refused = append(refused, *f)
		}
		if in.category.Valid {
			if f, err := lockReference("category_id", func() (bool, error) {
				return q.LockLiveCategoryShared(ctx, db.LockLiveCategorySharedParams{ID: in.category, HouseholdID: user.HouseholdID})
			}); err != nil {
				return err
			} else if f != nil {
				refused = append(refused, *f)
			}
		}
		if in.owner.Valid {
			_, err := q.LockLiveUserShared(ctx, db.LockLiveUserSharedParams{ID: in.owner, HouseholdID: user.HouseholdID})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				refused = append(refused, failure{"owner_user_id", "invalid"})
			case err != nil:
				return opaque("lock the owner", err)
			}
		}
		if len(refused) > 0 {
			return refusal{least(refused)}
		}

		occurredOn := in.occurredOn
		if !occurredOn.Valid {
			at, err := budget.Locate(ctx, q, h.now, user)
			if err != nil {
				return opaque("resolve today", err)
			}
			occurredOn = pgtype.Date{Time: at.Today.Time(), Valid: true}
		}

		created, err := q.CreateExpense(ctx, db.CreateExpenseParams{
			ID:           id,
			HouseholdID:  user.HouseholdID,
			OccurredOn:   occurredOn,
			OccurredTime: in.occurredTime,
			Amount:       in.amount,
			Currency:     currency,
			PocketID:     in.pocket,
			CategoryID:   in.category,
			Description:  in.description,
			Ownership:    in.ownership,
			OwnerUserID:  in.owner,
		})
		switch {
		case err == nil:
			row = created
			return nil
		case errors.Is(err, pgx.ErrNoRows):
			// The id is held, and was not a moment ago: a concurrent replay
			// that has committed since the read-back above.
			existing, err := q.GetLiveExpense(ctx, db.GetLiveExpenseParams{ID: id, HouseholdID: user.HouseholdID})
			switch {
			case err == nil:
				row, replay = existing, true
				return nil
			case errors.Is(err, pgx.ErrNoRows):
				return errIDTaken
			default:
				return opaque("read back", err)
			}
		default:
			return opaque("insert", err)
		}
	})

	var refused refusal
	switch {
	case err == nil && replay:
		return api.CreateExpense200JSONResponse(response(row)), nil
	case err == nil:
		return api.CreateExpense201JSONResponse(response(row)), nil
	case errors.Is(err, errIDTaken):
		return api.CreateExpense409JSONResponse{
			ConflictJSONResponse: api.ConflictJSONResponse{Code: api.IDTAKEN},
		}, nil
	case errors.As(err, &refused):
		return validation(refused.failure), nil
	default:
		logFailure("create expense", err)
		return api.CreateExpense500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}
}

// input is a CreateExpenseRequest once its own rules have passed, in the types
// the statements take.
type input struct {
	amount       decimal.Decimal
	occurredOn   pgtype.Date // not Valid: the writer's Today, resolved in the transaction
	occurredTime pgtype.Time
	pocket       pgtype.UUID
	category     pgtype.UUID
	description  *string
	ownership    string
	owner        pgtype.UUID
}

// failure is a field breaking a rule: the VALIDATION answer.
type failure struct{ field, rule string }

func (f failure) less(o failure) bool {
	if f.field != o.field {
		return f.field < o.field
	}
	return f.rule < o.rule
}

// least is the failure that sorts first by (field, rule), the one every
// request-body failure reports (BOOTSTRAP.md §5.2).
func least(fs []failure) failure {
	first := fs[0]
	for _, f := range fs[1:] {
		if f.less(first) {
			first = f
		}
	}
	return first
}

// checkBody is every rule of a create's body that the contract cannot state,
// judged before the database is asked anything. All that fail are collected and
// the least (field, rule) is reported.
func checkBody(body *api.CreateExpenseRequest) (input, *failure) {
	in := input{
		pocket:    pgtype.UUID{Bytes: body.PocketId, Valid: true},
		ownership: string(api.Joint),
	}
	var failures []failure

	// The contract's pattern admits only what parses. Zero is the one thing it
	// cannot say it refuses: `"0"`, `"0.0000"` and `"-0"` all pass it.
	amount, err := decimal.NewFromString(string(body.Amount))
	switch {
	case err != nil:
		failures = append(failures, failure{"amount", "pattern"})
	case amount.IsZero():
		failures = append(failures, failure{"amount", ruleNonzero})
	default:
		in.amount = amount
	}

	if body.OccurredOn != nil {
		date, err := period.ParseDate(*body.OccurredOn)
		// time.Parse reads year 0000, which Postgres has no date for.
		if err != nil || date.Year < 1 {
			failures = append(failures, failure{"occurred_on", "invalid"})
		} else {
			in.occurredOn = pgtype.Date{Time: date.Time(), Valid: true}
		}
	}

	if body.OccurredTime != nil {
		// HH:MM, so the same reader as a Household's day_starts_at.
		d, err := period.ParseDayStartsAt(*body.OccurredTime)
		if err != nil {
			failures = append(failures, failure{"occurred_time", "pattern"})
		} else {
			in.occurredTime = pgtype.Time{Microseconds: d.Microseconds(), Valid: true}
		}
	}

	if body.Description != nil {
		text, rule := auth.CheckBoundedName(*body.Description, maxDescriptionCodePoints)
		if rule != "" {
			failures = append(failures, failure{"description", rule})
		} else {
			in.description = &text
		}
	}

	if body.CategoryId != nil {
		in.category = pgtype.UUID{Bytes: *body.CategoryId, Valid: true}
	}

	// ownership and owner_user_id are a pair (#140 R6, X12): sole_owner names a
	// User, joint names none, and an owner with no ownership is half a pair.
	// Whether the User is the Household's is judged against the database.
	soleOwner := body.Ownership != nil && *body.Ownership == api.SoleOwner
	if soleOwner {
		in.ownership = string(api.SoleOwner)
	}
	if body.OwnerUserId != nil {
		in.owner = pgtype.UUID{Bytes: *body.OwnerUserId, Valid: true}
	}
	if soleOwner != (body.OwnerUserId != nil) {
		failures = append(failures, failure{"owner_user_id", "invalid"})
	}

	if len(failures) > 0 {
		f := least(failures)
		return input{}, &f
	}
	return in, nil
}

// ruleNonzero is the VALIDATION rule for an amount of zero (BOOTSTRAP.md §5.2).
// go-playground's own tag is `ne`, which says nothing to a reader of the
// envelope; the rule is named for the CHECK that is the net under it,
// `expenses_amount_nonzero`.
const ruleNonzero = "nonzero"

// lockReference share-locks a Pocket or Category the body names, in the
// statement that checks it is the caller's own and live, and judges it: no row
// is `invalid` (unknown, another Household's and deleted are one answer, so a
// foreign id is never told from a missing one), and a live one that is no longer
// offered for new capture is `inactive`. A row that is already in an inactive
// Pocket or Category is not looked at: only a new reference is judged.
func lockReference(field string, lock func() (active bool, err error)) (*failure, error) {
	active, err := lock()
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return &failure{field, "invalid"}, nil
	case err != nil:
		return nil, opaque("lock the "+field, err)
	case !active:
		return &failure{field, "inactive"}, nil
	}
	return nil, nil
}

// refusal is a reference that is not what the body said it was, carried out of
// the transaction so it rolls back (nothing was written before it).
type refusal struct{ failure failure }

func (r refusal) Error() string {
	return fmt.Sprintf("refused: %s %s", r.failure.field, r.failure.rule)
}

// errIDTaken is an id held by something other than a live Expense of the
// caller's own Household: another Household's, or one of its own that is
// deleted.
var errIDTaken = errors.New("the id is held")

// stepError is an error from one step of a transaction, flattened so that a
// pgx.ErrNoRows from a later statement is never read as the one the INSERT
// raised (errors.Is cannot see through it). A database error keeps its SQLSTATE
// and constraint name, which are all that is safe to log: the driver's own text
// can carry the offending row's values, and an Expense's are an amount and a
// description.
type stepError struct {
	text, sqlstate, constraint string
}

func (e *stepError) Error() string { return e.text }

// opaque keeps an error's text and drops its chain, except for a database
// error's SQLSTATE and constraint (see stepError).
func opaque(step string, err error) error {
	out := &stepError{text: fmt.Sprintf("%s: %v", step, err)}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		out.sqlstate, out.constraint = pgErr.Code, pgErr.ConstraintName
	}
	return out
}

// logFailure logs an error without anything of the request. A database error,
// whether it comes straight from the Querier or through opaque, is reduced to its
// SQLSTATE and constraint, never its text.
func logFailure(msg string, err error) {
	var step *stepError
	if errors.As(err, &step) && step.sqlstate != "" {
		slog.Error(msg, "sqlstate", step.sqlstate, "constraint", step.constraint)
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		slog.Error(msg, "sqlstate", pgErr.Code, "constraint", pgErr.ConstraintName)
		return
	}
	slog.Error(msg, "err", err)
}

// inTx runs fn in a transaction, committed only if fn returns nil. An error
// comes back as fn returned it. It runs on the request's context, so a client
// that goes away rolls it back and retrying with the same id is the recovery.
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

func validation(f failure) api.CreateExpenseResponseObject {
	return api.CreateExpense400JSONResponse{
		BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: api.VALIDATION,
			Args: &map[string]interface{}{"field": f.field, "rule": f.rule},
		},
	}
}

// response projects an Expense onto the wire. Money is a string at the storage
// scale, a refund keeping its sign (BOOTSTRAP.md §4): StringFixed(4), never
// String(). Optional properties are omitted when empty, never null.
func response(row db.Expense) api.Expense {
	out := api.Expense{
		Id:         openapi_types.UUID(row.ID.Bytes),
		OccurredOn: openapi_types.Date{Time: row.OccurredOn.Time},
		CapturedAt: row.CapturedAt.Time.UTC(),
		Amount:     row.Amount.StringFixed(4),
		Currency:   row.Currency,
		PocketId:   openapi_types.UUID(row.PocketID.Bytes),
		Ownership:  api.Ownership(row.Ownership),
	}
	if row.OccurredTime.Valid {
		t := formatTime(row.OccurredTime)
		out.OccurredTime = &t
	}
	if row.CategoryID.Valid {
		id := openapi_types.UUID(row.CategoryID.Bytes)
		out.CategoryId = &id
	}
	if row.Description != nil {
		d := *row.Description
		out.Description = &d
	}
	if row.OwnerUserID.Valid {
		id := openapi_types.UUID(row.OwnerUserID.Bytes)
		out.OwnerUserId = &id
	}
	return out
}

// formatTime is a time of day as HH:MM, the wire's spelling.
func formatTime(t pgtype.Time) string {
	minutes := t.Microseconds / int64(time.Minute/time.Microsecond)
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
