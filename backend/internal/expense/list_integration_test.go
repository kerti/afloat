package expense_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
)

// The list is newest first, keyset-paged, in the order #140 R7 declares:
// occurred_on DESC, the effective time DESC, captured_at DESC, id DESC. The User
// below is in Asia/Jakarta (UTC+7) and the Household rolls over at 04:00, so a
// Period Day runs 04:00 to 03:59 local.

// seed is one Expense row, written directly so every column that orders the list
// is chosen by the test.
type seed struct {
	id         uuid.UUID
	occurredOn string
	// occurredTime is "HH:MM", or "" for none.
	occurredTime string
	// capturedAt is an RFC 3339 instant.
	capturedAt string
	deleted    bool
	category   pgtype.UUID
}

func (f fixture) insert(t *testing.T, household, pocket pgtype.UUID, s seed) {
	t.Helper()
	var tm any
	if s.occurredTime != "" {
		tm = s.occurredTime
	}
	var deleted any
	if s.deleted {
		deleted = "2026-11-25T00:00:00Z"
	}
	f.exec(t, `INSERT INTO expenses (id, household_id, occurred_on, occurred_time, captured_at, amount, currency, pocket_id, category_id, deleted_at)
		VALUES ($1, $2, $3::date, $4::time, $5::timestamptz, 25000, 'IDR', $6, $7, $8::timestamptz)`,
		s.id, household, s.occurredOn, tm, s.capturedAt, pocket, s.category, deleted)
}

// sid is an id whose last digits order it, so a test can name a row and know
// which way `id DESC` sorts it.
func sid(n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("019212e0-0000-7000-8000-%012d", n))
}

func (f fixture) list(ctx context.Context, t *testing.T, limit *int, after *string) api.ExpenseList {
	t.Helper()
	resp, err := f.handlers.ListExpenses(ctx, api.ListExpensesRequestObject{Params: api.ListExpensesParams{Limit: limit, After: after}})
	if err != nil {
		t.Fatalf("ListExpenses returned an error: %v", err)
	}
	got, ok := resp.(api.ListExpenses200JSONResponse)
	if !ok {
		t.Fatalf("ListExpenses = %s, want 200", status(resp))
	}
	return api.ExpenseList(got)
}

func ids(page api.ExpenseList) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(page.Items))
	for _, e := range page.Items {
		out = append(out, e.Id)
	}
	return out
}

// walk reads every page at the given size and returns the rows in the order
// they came, with the number of pages.
func (f fixture) walk(ctx context.Context, t *testing.T, limit int) ([]uuid.UUID, int) {
	t.Helper()
	var all []uuid.UUID
	var after *string
	for pages := 1; ; pages++ {
		page := f.list(ctx, t, &limit, after)
		all = append(all, ids(page)...)
		if page.Next == nil {
			return all, pages
		}
		if pages > 200 {
			t.Fatal("paging does not end")
		}
		after = page.Next
	}
}

func TestAnEmptyHouseholdIsAnEmptyItemsArrayAndNoNext(t *testing.T) {
	f := newFixture(t)
	_, _, ctx := f.member(t, "Home", "a@example.com")
	page := f.list(ctx, t, nil, nil)
	if got := wire(t, page); got != `{"items":[]}` {
		t.Errorf("wire = %s, want {\"items\":[]}", got)
	}
}

// orderFixture is contract/testdata/expense_order.json: the order of the list and
// where a keyset page may end, as data (BOOTSTRAP.md §6).
type orderFixture struct {
	Description string `json:"description"`
	Scenarios   []struct {
		Name        string `json:"name"`
		TimeZone    string `json:"time_zone"`
		DayStartsAt string `json:"day_starts_at"`
		Rows        []struct {
			N            int    `json:"n"`
			Name         string `json:"name"`
			OccurredOn   string `json:"occurred_on"`
			OccurredTime string `json:"occurred_time"`
			CapturedAt   string `json:"captured_at"`
			Deleted      bool   `json:"deleted"`
			Other        bool   `json:"other"`
		} `json:"rows"`
		Order []string `json:"order"`
		Pages []struct {
			Limit int        `json:"limit"`
			Pages [][]string `json:"pages"`
		} `json:"pages"`
	} `json:"scenarios"`
}

// The order, row by row, from the fixture: date, then the effective time (an
// offset into the Period Day, so 01:30 sorts after 23:00 at a 04:00 start), then
// captured_at, then id; across a spring-forward and a fall-back day, where the
// capture's own wall clock decides whether it was captured on the Period Day it
// is dated. Every page size is walked, so a boundary falls on every row, and the
// fixture's explicit pages are held to as well.
func TestTheOrderAndItsPageBoundariesAreTheFixtures(t *testing.T) {
	raw, err := os.ReadFile("../../../contract/testdata/expense_order.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx orderFixture
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fx); err != nil {
		t.Fatalf("expense_order.json: %v", err)
	}
	if len(fx.Scenarios) == 0 {
		t.Fatal("expense_order.json holds no scenario")
	}

	for _, sc := range fx.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			f := newFixture(t)
			household, pocket, ctx := f.member(t, "Home", "a@example.com")
			other := f.tdb.CreateHousehold(t, "Elsewhere")
			theirPocket := f.tdb.CreatePocket(t, other, "Theirs")
			f.exec(t, `UPDATE users SET time_zone = $1 WHERE household_id = $2`, sc.TimeZone, household)
			f.exec(t, `UPDATE households SET day_starts_at = $1::time WHERE id = $2`, sc.DayStartsAt, household)
			user, err := f.tdb.Queries.GetUserByID(context.Background(), mustUser(t, f))
			if err != nil {
				t.Fatal(err)
			}
			ctx = auth.WithUser(ctx, user)

			names := map[uuid.UUID]string{}
			for _, r := range sc.Rows {
				names[sid(r.N)] = r.Name
				s := seed{id: sid(r.N), occurredOn: r.OccurredOn, occurredTime: r.OccurredTime, capturedAt: r.CapturedAt, deleted: r.Deleted}
				if r.Other {
					f.insert(t, other, theirPocket, s)
				} else {
					f.insert(t, household, pocket, s)
				}
			}
			named := func(in []uuid.UUID) []string {
				out := make([]string, 0, len(in))
				for _, id := range in {
					out = append(out, names[id])
				}
				return out
			}

			full, pages := f.walk(ctx, t, 100)
			if pages != 1 {
				t.Fatalf("%d pages for a limit of 100", pages)
			}
			if got := named(full); !slices.Equal(got, sc.Order) {
				t.Fatalf("order:\n got %q\nwant %q", got, sc.Order)
			}
			for limit := 1; limit <= len(sc.Order)+1; limit++ {
				if got, _ := f.walk(ctx, t, limit); !slices.Equal(named(got), sc.Order) {
					t.Errorf("limit %d: pages gave\n%q\nwant\n%q", limit, named(got), sc.Order)
				}
			}
			for _, want := range sc.Pages {
				var after *string
				for i, wantPage := range want.Pages {
					page := f.list(ctx, t, ptr(want.Limit), after)
					if got := named(ids(page)); !slices.Equal(got, wantPage) {
						t.Errorf("limit %d, page %d = %q, want %q", want.Limit, i+1, got, wantPage)
					}
					if last := i == len(want.Pages)-1; last != (page.Next == nil) {
						t.Errorf("limit %d, page %d: next present = %v", want.Limit, i+1, page.Next != nil)
					}
					after = page.Next
				}
			}
		})
	}
}

// The red proof, kept: a cursor with no id tie-break skips every row equal to the
// last one on the page. If this ever stops failing the paging, the id is no longer
// what keeps a boundary exact and the keyset needs a rethink, not a quiet
// deletion.
var idTuple = regexp.MustCompile(`, \$(\d+)::uuid\)`)

func TestWithoutTheIDTieBreakAPageBoundaryOnEqualKeysSkipsRows(t *testing.T) {
	f := newFixtureWith(t, func(tx db.DBTX) db.DBTX {
		return hooked{DBTX: tx, rewrite: func(sql string) string {
			if !strings.Contains(sql, marker(listQuery)) {
				return sql
			}
			if !strings.Contains(sql, "(k.time_key, e.captured_at, e.id)") || !idTuple.MatchString(sql) {
				panic("the list's cursor comparison is no longer the shape the red proof rewrites")
			}
			param := idTuple.FindStringSubmatch(sql)[1]
			sql = strings.Replace(sql, "(k.time_key, e.captured_at, e.id)", "(k.time_key, e.captured_at)", 1)
			sql = idTuple.ReplaceAllString(sql, ")")
			// The parameter must still be used, or Postgres cannot type it.
			return strings.Replace(sql, "ORDER BY", "AND ($"+param+"::uuid IS NULL OR true)\nORDER BY", 1)
		}}
	})
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	for i := 1; i <= 6; i++ {
		f.insert(t, household, pocket, seed{id: sid(i), occurredOn: "2026-11-23", occurredTime: "10:00", capturedAt: "2026-11-23T05:00:00Z"})
	}

	got, _ := f.walk(ctx, t, 2)
	if len(got) >= 6 {
		t.Errorf("paging without the id tie-break read all %d rows; it was expected to skip the ones equal to a page's last", len(got))
	}
}

func TestTheDefaultPageIs25AndNextIsOmittedOnTheLastPage(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	for i := 1; i <= 30; i++ {
		f.insert(t, household, pocket, seed{id: sid(i), occurredOn: "2026-11-23", capturedAt: fmt.Sprintf("2026-11-23T05:%02d:00Z", i)})
	}

	first := f.list(ctx, t, nil, nil)
	if len(first.Items) != 25 || first.Next == nil {
		t.Fatalf("first page: %d items, next %v; want 25 and a cursor", len(first.Items), first.Next)
	}
	second := f.list(ctx, t, nil, first.Next)
	if len(second.Items) != 5 || second.Next != nil {
		t.Fatalf("second page: %d items, next %v; want 5 and none", len(second.Items), second.Next)
	}
	if strings.Contains(wire(t, second), `"next"`) {
		t.Errorf("next is on the wire of the last page: %s", wire(t, second))
	}

	// Exactly a full page is a last page: nothing follows, so no cursor.
	ten := 30
	if page := f.list(ctx, t, &ten, nil); page.Next != nil {
		t.Errorf("a page holding every row carries a cursor: %v", *page.Next)
	}
}

func TestOnlyLiveRowsOfTheCallersHouseholdAreListed(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	other := f.tdb.CreateHousehold(t, "Elsewhere")
	theirPocket := f.tdb.CreatePocket(t, other, "Theirs")
	f.insert(t, household, pocket, seed{id: sid(1), occurredOn: "2026-11-23", capturedAt: "2026-11-23T05:00:00Z"})
	f.insert(t, household, pocket, seed{id: sid(2), occurredOn: "2026-11-23", capturedAt: "2026-11-23T05:00:00Z", deleted: true})
	f.insert(t, other, theirPocket, seed{id: sid(3), occurredOn: "2026-11-23", capturedAt: "2026-11-23T05:00:00Z"})

	page := f.list(ctx, t, nil, nil)
	if got := ids(page); !slices.Equal(got, []uuid.UUID{sid(1)}) {
		t.Errorf("listed %v, want only %v", got, sid(1))
	}

	// A cursor from the other Household moves the reader through the reader's own
	// rows and no others: it carries a sort key, not a tenant.
	f.insert(t, household, pocket, seed{id: sid(4), occurredOn: "2026-11-22", capturedAt: "2026-11-22T05:00:00Z"})
	theirs := f.userIn(t, other, "b@example.com")
	theirPage := f.list(theirs, t, ptr(1), nil)
	if len(theirPage.Items) != 1 || theirPage.Items[0].Id != sid(3) {
		t.Fatalf("their page = %v", ids(theirPage))
	}
	f.insert(t, other, theirPocket, seed{id: sid(5), occurredOn: "2026-11-22", capturedAt: "2026-11-22T05:00:00Z"})
	theirPage = f.list(theirs, t, ptr(1), nil)
	mine := f.list(ctx, t, nil, theirPage.Next)
	for _, e := range mine.Items {
		if e.Id == sid(3) || e.Id == sid(5) {
			t.Errorf("another Household's Expense %v was listed", e.Id)
		}
	}
}

func TestEveryRowCarriesTheWireShapeOfAnExpense(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	category := f.tdb.CreateCategory(t, household, "Food", "daily")
	f.insert(t, household, pocket, seed{id: sid(1), occurredOn: "2026-11-23", occurredTime: "19:05", capturedAt: "2026-11-23T05:00:00Z", category: category})
	f.exec(t, `UPDATE expenses SET amount = -450000, description = 'Refund' WHERE id = $1`, sid(1))

	page := f.list(ctx, t, nil, nil)
	if len(page.Items) != 1 {
		t.Fatalf("%d items", len(page.Items))
	}
	e := page.Items[0]
	if e.Amount != "-450000.0000" || e.Currency != "IDR" || e.OccurredTime == nil || *e.OccurredTime != "19:05" ||
		e.Description == nil || *e.Description != "Refund" || e.CategoryId == nil || e.Ownership != api.Joint {
		t.Errorf("row = %s", wire(t, e))
	}
	if !e.CapturedAt.Equal(time.Date(2026, 11, 23, 5, 0, 0, 0, time.UTC)) {
		t.Errorf("captured_at = %v", e.CapturedAt)
	}
}

func TestACursorThisOperationDidNotProduceIsAfterInvalid(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	f.insert(t, household, pocket, seed{id: sid(1), occurredOn: "2026-11-23", capturedAt: "2026-11-23T05:00:00Z"})
	f.insert(t, household, pocket, seed{id: sid(2), occurredOn: "2026-11-23", capturedAt: "2026-11-23T05:00:00Z"})
	good := f.list(ctx, t, ptr(1), nil)
	if good.Next == nil {
		t.Fatal("no cursor to start from")
	}
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	valid := `{"o":"2026-11-23","k":3600000000,"c":1790000000000000,"i":"019212e0-0000-7000-8000-000000000001"}`

	for name, after := range map[string]string{
		"empty":                   "",
		"not base64":              "!!!!",
		"padded base64":           base64.URLEncoding.EncodeToString([]byte(valid)),
		"standard alphabet":       strings.NewReplacer("-", "+", "_", "/").Replace(enc(`{"o":"2026-11-23","k":1,"c":1,"i":"019212e0-0000-7000-8000-000000000001","x":"~~~>>>"}`)),
		"not json":                enc("hello"),
		"an array":                enc(`[1,2,3,4]`),
		"an empty object":         enc(`{}`),
		"a missing key":           enc(`{"o":"2026-11-23","k":0,"c":1790000000000000}`),
		"an unknown key":          enc(`{"o":"2026-11-23","k":0,"c":1790000000000000,"i":"019212e0-0000-7000-8000-000000000001","x":1}`),
		"trailing data":           enc(valid + `{}`),
		"trailing garbage":        enc(valid + ` x`),
		"a null":                  enc(`{"o":null,"k":0,"c":1,"i":"019212e0-0000-7000-8000-000000000001"}`),
		"a date that is not":      enc(`{"o":"2026-02-30","k":0,"c":1790000000000000,"i":"019212e0-0000-7000-8000-000000000001"}`),
		"year zero":               enc(`{"o":"0000-01-01","k":0,"c":1790000000000000,"i":"019212e0-0000-7000-8000-000000000001"}`),
		"a short date":            enc(`{"o":"2026-1-5","k":0,"c":1790000000000000,"i":"019212e0-0000-7000-8000-000000000001"}`),
		"a negative time":         enc(`{"o":"2026-11-23","k":-1,"c":1790000000000000,"i":"019212e0-0000-7000-8000-000000000001"}`),
		"a time past the day":     enc(`{"o":"2026-11-23","k":86400000000,"c":1790000000000000,"i":"019212e0-0000-7000-8000-000000000001"}`),
		"an instant out of range": enc(`{"o":"2026-11-23","k":0,"c":9223372036854775807,"i":"019212e0-0000-7000-8000-000000000001"}`),
		"an id that is not":       enc(`{"o":"2026-11-23","k":0,"c":1790000000000000,"i":"nope"}`),
		"an id spelt another way": enc(`{"o":"2026-11-23","k":0,"c":1790000000000000,"i":"019212E0-0000-7000-8000-000000000001"}`),
		"too long":                strings.Repeat("A", 513),
	} {
		resp, err := f.handlers.ListExpenses(ctx, api.ListExpensesRequestObject{Params: api.ListExpensesParams{After: ptr(after)}})
		if err != nil {
			t.Fatal(err)
		}
		if got := status(resp); got != "VALIDATION after invalid" {
			t.Errorf("%s answered %s, want VALIDATION after invalid", name, got)
		}
	}
	// And the one it did produce works, so the table above is not refusing
	// everything.
	if _, err := f.handlers.ListExpenses(ctx, api.ListExpensesRequestObject{Params: api.ListExpensesParams{After: good.Next}}); err != nil {
		t.Fatal(err)
	}
	if got := f.list(ctx, t, ptr(1), good.Next); len(got.Items) != 1 {
		t.Errorf("the second page held %d rows", len(got.Items))
	}
}

func TestALimitOutsideOneToAHundredIsRefused(t *testing.T) {
	f := newFixture(t)
	_, _, ctx := f.member(t, "Home", "a@example.com")
	for limit, want := range map[int]string{-1: "VALIDATION limit min", 0: "VALIDATION limit min", 101: "VALIDATION limit max", 1000: "VALIDATION limit max"} {
		resp, err := f.handlers.ListExpenses(ctx, api.ListExpensesRequestObject{Params: api.ListExpensesParams{Limit: ptr(limit)}})
		if err != nil {
			t.Fatal(err)
		}
		if got := status(resp); got != want {
			t.Errorf("limit %d answered %s, want %s", limit, got, want)
		}
	}
	f.list(ctx, t, ptr(1), nil)
	f.list(ctx, t, ptr(100), nil)
}

// The effective time is read in the caller's zone and the Household's rollover,
// so a change of either re-sorts the rows it touches (stated in the contract).
func TestTheEffectiveTimeFollowsTheCallersZoneAndTheHouseholdsRollover(t *testing.T) {
	f := newFixture(t)
	household, pocket, ctx := f.member(t, "Home", "a@example.com")
	// Captured 2026-11-23T03:00Z, dated the 23rd, with no time given.
	f.insert(t, household, pocket, seed{id: sid(1), occurredOn: "2026-11-23", capturedAt: "2026-11-23T03:00:00Z"})
	// Given 08:00, which is offset 04:00 at a 04:00 rollover.
	f.insert(t, household, pocket, seed{id: sid(2), occurredOn: "2026-11-23", occurredTime: "08:00", capturedAt: "2026-11-20T03:00:00Z"})

	// Jakarta: captured at 10:00 (offset 06:00) is after the 08:00 one.
	if got := ids(f.list(ctx, t, nil, nil)); !slices.Equal(got, []uuid.UUID{sid(1), sid(2)}) {
		t.Errorf("Jakarta: %v", got)
	}
	// London (UTC+0 in November): the capture reads 03:00 on the 23rd, still the
	// 22nd's Period Day, so it is no longer "captured that same day": the start of
	// the day, below the 08:00.
	f.exec(t, `UPDATE users SET time_zone = 'Europe/London'`)
	user, err := f.tdb.Queries.GetUserByID(context.Background(), mustUser(t, f))
	if err != nil {
		t.Fatal(err)
	}
	ctxLondon := auth.WithUser(ctx, user)
	if got := ids(f.list(ctxLondon, t, nil, nil)); !slices.Equal(got, []uuid.UUID{sid(2), sid(1)}) {
		t.Errorf("London: %v", got)
	}
}

func mustUser(t *testing.T, f fixture) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT id FROM users LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
