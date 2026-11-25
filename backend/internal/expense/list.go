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
// newest first (#140 R7). The Household is the authenticated User's, taken from
// the context and never from the request, and the SELECT filters on it in SQL.
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

	rows, err := h.q.ListExpenses(ctx, params)
	if err != nil {
		logFailure("list expenses", err)
		return api.ListExpenses500JSONResponse{
			InternalErrorJSONResponse: api.InternalErrorJSONResponse{Code: api.INTERNAL},
		}, nil
	}

	out := api.ExpenseList{Items: make([]api.Expense, 0, min(len(rows), limit))}
	for i, row := range rows {
		if i == limit {
			// The extra row is only the proof that there is another page.
			last := rows[limit-1]
			next := encodeCursor(last)
			out.Next = &next
			break
		}
		out.Items = append(out.Items, response(row.Expense))
	}
	return api.ListExpenses200JSONResponse(out), nil
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

func encodeCursor(row db.ListExpensesRow) string {
	on := period.DateOf(row.Expense.OccurredOn.Time).String()
	key := row.TimeKey.Microseconds
	captured := row.Expense.CapturedAt.Time.UnixMicro()
	id := uuid.UUID(row.Expense.ID.Bytes).String()
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
