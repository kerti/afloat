package expense

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/period"
)

const (
	defaultLimit = 25
	minLimit     = 1
	maxLimit     = 100

	// maxCursorBytes bounds what is decoded, so a pasted megabyte is `invalid`
	// before anything reads it. A real cursor is about 100 bytes.
	maxCursorBytes = 512

	microsPerDay = int64(24 * time.Hour / time.Microsecond)
)

var (
	minCapturedAtMicros = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC).UnixMicro()
	maxCapturedAtMicros = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC).UnixMicro()
)

// ListExpenses answers one page of the caller's Household's live Expenses,
// newest first (#140 R7), or of those with no Category with ?uncategorised=true
// (R8), and the Household's count of them either way; or, with ?deleted=true,
// one page of its Recycle Bin (R11) and no count. The Household is the
// authenticated User's, taken from the context and never from the request, and
// every SELECT filters on it in SQL.
//
// The order is declared in the statement (queries/expense.sql), with id as its
// last key so a page boundary is exact over rows equal in every other. It is
// keyset-paged: the cursor carries the last row's sort key, the computed
// effective time included, because a page boundary that fell on a wrapped time
// would otherwise repeat rows. A cursor is opaque to the client and is not
// signed: it can only move the reader through their own Household's rows, since
// every statement filters on household_id.
func (h *Handlers) ListExpenses(ctx context.Context, request api.ListExpensesRequestObject) (api.ListExpensesResponseObject, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		// Unreachable through the router: httpserver's sessionRequired answers
		// 401 for this operation first. Kept so a router change that lost the
		// check degrades to the contract's 401, not a SELECT with a zero
		// household_id.
		return api.ListExpenses401JSONResponse{
			UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.UNAUTHORIZED},
		}, nil
	}

	limit := defaultLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	// The contract's own range check answers first through the router; this is
	// for a caller that does not go through it.
	if limit < minLimit {
		return listValidation("limit", "min"), nil
	}
	if limit > maxLimit {
		return listValidation("limit", "max"), nil
	}

	if request.Params.Deleted != nil && *request.Params.Deleted {
		return h.listBin(ctx, user, request.Params, limit)
	}

	params := db.ListExpensesParams{
		TimeZone:    user.TimeZone,
		HouseholdID: user.HouseholdID,
		// One more than the page, so a last page is told from a full one.
		PageSize: int32(limit + 1),
	}
	if request.Params.After != nil {
		c, err := decodeCursor(*request.Params.After)
		if err != nil {
			return listValidation("after", "invalid"), nil
		}
		params.AfterOccurredOn = pgtype.Date{Time: c.occurredOn.Time(), Valid: true}
		params.AfterTimeKey = pgtype.Time{Microseconds: c.timeKey, Valid: true}
		params.AfterCapturedAt = pgtype.Timestamptz{Time: time.UnixMicro(c.capturedAt).UTC(), Valid: true}
		params.AfterID = pgtype.UUID{Bytes: c.id, Valid: true}
	}

	rows, err := h.listRows(ctx, params, request.Params.Uncategorised != nil && *request.Params.Uncategorised)
	if err != nil {
		logFailure("list expenses", err)
		return api.ListExpenses500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}

	// The count is its own statement, after the page and outside any
	// transaction: a write landing between the two can leave it one off from the
	// rows beside it, which the next read corrects and nothing is decided on.
	// The Recycle Bin read does not carry it (listBin).
	count, err := h.q.CountUncategorisedExpenses(ctx, user.HouseholdID)
	if err != nil {
		logFailure("count uncategorised expenses", err)
		return api.ListExpenses500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}

	n := int(count)
	out := api.ExpenseList{
		Items:              make([]api.Expense, 0, min(len(rows), limit)),
		UncategorisedCount: &n,
	}
	for i, row := range rows {
		if i == limit {
			// The extra row is only the proof that there is another page.
			next := encodeCursor(rows[limit-1])
			out.Next = &next
			break
		}
		out.Items = append(out.Items, response(row.expense))
	}
	return api.ListExpenses200JSONResponse(out), nil
}

// listBin is the Recycle Bin read (#140 R11): the Household's deleted Expenses,
// deleted_at then id descending, keyset-paged by its own cursor, and no
// uncategorised_count (it counts live rows, and the bin lists none).
//
// The bin has no filter, so ?uncategorised=true beside it is refused as
// {uncategorised, invalid} and never answered as an unfiltered bin. A bad
// `after` is judged first: the failures that compete are reported least
// (field, rule), and `after` sorts before `uncategorised` (BOOTSTRAP.md §5.2).
func (h *Handlers) listBin(ctx context.Context, user db.User, p api.ListExpensesParams, limit int) (api.ListExpensesResponseObject, error) {
	params := db.ListDeletedExpensesParams{
		HouseholdID: user.HouseholdID,
		PageSize:    int32(limit + 1),
	}
	if p.After != nil {
		c, err := decodeBinCursor(*p.After)
		if err != nil {
			return listValidation("after", "invalid"), nil
		}
		params.AfterDeletedAt = pgtype.Timestamptz{Time: time.UnixMicro(c.deletedAt).UTC(), Valid: true}
		params.AfterID = pgtype.UUID{Bytes: c.id, Valid: true}
	}
	if p.Uncategorised != nil && *p.Uncategorised {
		return listValidation("uncategorised", "invalid"), nil
	}

	rows, err := h.q.ListDeletedExpenses(ctx, params)
	if err != nil {
		logFailure("list deleted expenses", err)
		return api.ListExpenses500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}

	out := api.ExpenseList{Items: make([]api.Expense, 0, min(len(rows), limit))}
	for i, row := range rows {
		if i == limit {
			next := encodeBinCursor(rows[limit-1])
			out.Next = &next
			break
		}
		out.Items = append(out.Items, response(row))
	}
	return api.ListExpenses200JSONResponse(out), nil
}

// listRow is a row of either list: the Expense and the effective time it was
// sorted by, which the cursor carries.
type listRow struct {
	expense db.Expense
	timeKey pgtype.Time
}

// listRows runs the statement for the whole live list, or for the one with no
// Category. They take the same arguments and answer the same columns; they are
// two statements so the second names `category_id IS NULL` in its text and the
// partial index can serve it (see ListUncategorisedExpenses).
func (h *Handlers) listRows(ctx context.Context, params db.ListExpensesParams, uncategorised bool) ([]listRow, error) {
	if uncategorised {
		rows, err := h.q.ListUncategorisedExpenses(ctx, db.ListUncategorisedExpensesParams(params))
		if err != nil {
			return nil, err
		}
		out := make([]listRow, len(rows))
		for i, r := range rows {
			out[i] = listRow{expense: r.Expense, timeKey: r.TimeKey}
		}
		return out, nil
	}
	rows, err := h.q.ListExpenses(ctx, params)
	if err != nil {
		return nil, err
	}
	out := make([]listRow, len(rows))
	for i, r := range rows {
		out[i] = listRow{expense: r.Expense, timeKey: r.TimeKey}
	}
	return out, nil
}

func listValidation(field, rule string) api.ListExpensesResponseObject {
	return api.ListExpenses400JSONResponse{
		BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: api.VALIDATION,
			Args: &map[string]interface{}{"field": field, "rule": rule},
		},
	}
}

// cursor is the last row of a page's sort key, before the id breaks every tie.
type cursor struct {
	occurredOn period.Date
	// timeKey is the effective time as microseconds into its Period Day, the
	// computed key the order sorts by, so the next page resumes exactly after
	// the last row even where that key was derived rather than stored.
	timeKey    int64
	capturedAt int64 // microseconds since the Unix epoch, as Postgres holds it
	id         uuid.UUID
}

// wireCursor is the cursor's one JSON spelling, base64url without padding. A
// field is a pointer so that an absent one is told from a zero.
type wireCursor struct {
	OccurredOn *string `json:"o"`
	TimeKey    *int64  `json:"k"`
	CapturedAt *int64  `json:"c"`
	ID         *string `json:"i"`
}

func encodeCursor(row listRow) string {
	on := period.DateOf(row.expense.OccurredOn.Time).String()
	key := row.timeKey.Microseconds
	captured := row.expense.CapturedAt.Time.UnixMicro()
	id := uuid.UUID(row.expense.ID.Bytes).String()
	raw, err := json.Marshal(wireCursor{OccurredOn: &on, TimeKey: &key, CapturedAt: &captured, ID: &id})
	if err != nil {
		// A struct of strings and integers cannot fail to marshal.
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

var errCursor = errors.New("not a cursor this operation produced")

// decodeCursor accepts exactly what encodeCursor writes and nothing else: the
// alphabet, one JSON object with the four keys and no others, no trailing data,
// a real calendar date with a year, a time of day inside a day, and a canonical
// UUID. Anything else is `after` / `invalid`.
func decodeCursor(s string) (cursor, error) {
	if len(s) > maxCursorBytes {
		return cursor{}, errCursor
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		return cursor{}, errCursor
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var w wireCursor
	if err := dec.Decode(&w); err != nil {
		return cursor{}, errCursor
	}
	// A second value after the first, or garbage after it, is not ours.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return cursor{}, errCursor
	}
	if w.OccurredOn == nil || w.TimeKey == nil || w.CapturedAt == nil || w.ID == nil {
		return cursor{}, errCursor
	}
	date, err := period.ParseDate(*w.OccurredOn)
	if err != nil || date.Year < 1 || date.String() != *w.OccurredOn {
		return cursor{}, errCursor
	}
	if *w.TimeKey < 0 || *w.TimeKey >= microsPerDay {
		return cursor{}, errCursor
	}
	// Inside the years a timestamptz round-trips through Go, so that no integer
	// can reach the driver as an instant it cannot encode.
	if *w.CapturedAt < minCapturedAtMicros || *w.CapturedAt > maxCapturedAtMicros {
		return cursor{}, errCursor
	}
	id, err := uuid.Parse(*w.ID)
	if err != nil || id.String() != *w.ID {
		return cursor{}, errCursor
	}
	return cursor{occurredOn: date, timeKey: *w.TimeKey, capturedAt: *w.CapturedAt, id: id}, nil
}

// binCursor is the last row of a Recycle Bin page's sort key: when it was
// deleted, then its id. A different shape from the live list's, on purpose: a
// cursor names a position in ONE order, so each decoder refuses the other's.
type binCursor struct {
	deletedAt int64 // microseconds since the Unix epoch, as Postgres holds it
	id        uuid.UUID
}

type wireBinCursor struct {
	DeletedAt *int64  `json:"d"`
	ID        *string `json:"i"`
}

func encodeBinCursor(row db.Expense) string {
	deleted := row.DeletedAt.Time.UnixMicro()
	id := uuid.UUID(row.ID.Bytes).String()
	raw, err := json.Marshal(wireBinCursor{DeletedAt: &deleted, ID: &id})
	if err != nil {
		// A struct of a string and an integer cannot fail to marshal.
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeBinCursor accepts exactly what encodeBinCursor writes, under the rules
// decodeCursor holds the live one to: the alphabet, one JSON object with the two
// keys and no others, no trailing data, an instant a timestamptz round-trips,
// and a canonical UUID. Anything else, the live list's cursor included, is
// `after` / `invalid`.
func decodeBinCursor(s string) (binCursor, error) {
	if len(s) > maxCursorBytes {
		return binCursor{}, errCursor
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		return binCursor{}, errCursor
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var w wireBinCursor
	if err := dec.Decode(&w); err != nil {
		return binCursor{}, errCursor
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return binCursor{}, errCursor
	}
	if w.DeletedAt == nil || w.ID == nil {
		return binCursor{}, errCursor
	}
	if *w.DeletedAt < minCapturedAtMicros || *w.DeletedAt > maxCapturedAtMicros {
		return binCursor{}, errCursor
	}
	id, err := uuid.Parse(*w.ID)
	if err != nil || id.String() != *w.ID {
		return binCursor{}, errCursor
	}
	return binCursor{deletedAt: *w.DeletedAt, id: id}, nil
}
