package dashboard_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/dashboard"
	"github.com/kerti/afloat/backend/internal/db"
)

// The fold itself, on rows built by hand: no database, so a case that would be
// awkward to seed (a tie, an id the database never minted) is exact.

func uid(last byte) pgtype.UUID {
	var b [16]byte
	b[6], b[8], b[15] = 0x70, 0x80, last // a UUIDv7's version and variant, so it is a plausible id
	return pgtype.UUID{Bytes: b, Valid: true}
}

func spendRow(t *testing.T, day string, category pgtype.UUID, name string, active bool, bucket string, pocket pgtype.UUID, pocketName string, amount string) db.PeriodSpendRow {
	t.Helper()
	r := db.PeriodSpendRow{
		OccurredOn: pgd(t, day), CategoryID: category, PocketID: pocket, Bucket: bucket,
		PocketDisplayName: pocketName, PocketActive: true, Amount: decimal.RequireFromString(amount),
	}
	if category.Valid {
		r.CategoryDisplayName, r.CategoryActive = &name, &active
	}
	return r
}

func allocationRow(category pgtype.UUID, bucket, amount string) db.AllocationsAsOfRow {
	return db.AllocationsAsOfRow{CategoryID: category, Bucket: bucket, Allocation: decimal.NullDecimal{Decimal: decimal.RequireFromString(amount), Valid: true}}
}

func liveCategory(id pgtype.UUID, name string, active bool) db.Category {
	return db.Category{ID: id, CategoryDisplayName: name, Bucket: "daily", Active: active}
}

func TestFoldBreakdownCountsOnlyWhatIsOnOrBeforeToday(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	food, wallet := uid(1), uid(10)
	got, err := dashboard.FoldBreakdown([]db.PeriodSpendRow{
		spendRow(t, "2026-11-13", food, "Food", true, "daily", wallet, "Wallet", "100"),
		spendRow(t, "2026-11-14", food, "Food", true, "daily", wallet, "Wallet", "7"),
		spendRow(t, "2026-11-15", food, "Food", true, "daily", wallet, "Wallet", "50"),   // future
		spendRow(t, "2026-11-20", food, "Food", true, "daily", wallet, "Wallet", "-900"), // a future refund lowers nothing
	}, nil, nil, at)
	if err != nil {
		t.Fatalf("FoldBreakdown: %v", err)
	}
	if len(got.Categories) != 1 || got.Categories[0].Spent.String() != "107" {
		t.Errorf("categories = %+v, want Food at 107", got.Categories)
	}
	if len(got.Pockets) != 1 || got.Pockets[0].Spent.String() != "107" {
		t.Errorf("pockets = %+v, want Wallet at 107: the two lists add up the same Expenses", got.Pockets)
	}
}

// A row dated wholly in the future leaves no row at all: "with spend" means
// spend that counts.
func TestFoldBreakdownHasNoRowForACategoryOrPocketWhoseOnlySpendIsInTheFuture(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	got, err := dashboard.FoldBreakdown([]db.PeriodSpendRow{
		spendRow(t, "2026-11-20", uid(1), "Food", true, "daily", uid(10), "Wallet", "50"),
		spendRow(t, "2026-11-20", pgtype.UUID{}, "", false, "daily", uid(10), "Wallet", "5"),
	}, nil, nil, at)
	if err != nil {
		t.Fatalf("FoldBreakdown: %v", err)
	}
	if len(got.Categories) != 0 || len(got.Pockets) != 0 {
		t.Errorf("breakdown = %+v, want neither list", got)
	}
}

func TestFoldBreakdownLeavesACommitmentOutOfCategoriesAndKeepsItInPockets(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	food, rent, wallet, card := uid(1), uid(2), uid(10), uid(11)
	got, err := dashboard.FoldBreakdown([]db.PeriodSpendRow{
		spendRow(t, "2026-11-05", rent, "Rent", true, "commitment", card, "Card", "3000"),
		spendRow(t, "2026-11-06", food, "Food", true, "daily", wallet, "Wallet", "10"),
	}, []db.Category{liveCategory(food, "Food", true), {ID: rent, CategoryDisplayName: "Rent", Bucket: "commitment", Active: true}},
		[]db.AllocationsAsOfRow{allocationRow(rent, "commitment", "3000"), allocationRow(food, "daily", "500")}, at)
	if err != nil {
		t.Fatalf("FoldBreakdown: %v", err)
	}
	if len(got.Categories) != 1 || got.Categories[0].ID != food {
		t.Errorf("categories = %+v, want Food alone: a Commitment is in the Commitments panel, not here", got.Categories)
	}
	if len(got.Pockets) != 2 || got.Pockets[0].ID != card || got.Pockets[0].Spent.String() != "3000" {
		t.Errorf("pockets = %+v, want Card (3000) then Wallet: a Pocket is where money went, whatever it was for", got.Pockets)
	}
}

func TestFoldBreakdownGivesUncategorisedSpendItsOwnRowWithNoIdentityAndNoAllocation(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	got, err := dashboard.FoldBreakdown([]db.PeriodSpendRow{
		spendRow(t, "2026-11-05", pgtype.UUID{}, "", false, "daily", uid(10), "Wallet", "20"),
		spendRow(t, "2026-11-06", pgtype.UUID{}, "", false, "daily", uid(11), "Card", "25"),
	}, nil, nil, at)
	if err != nil {
		t.Fatalf("FoldBreakdown: %v", err)
	}
	if len(got.Categories) != 1 {
		t.Fatalf("categories = %+v, want the one uncategorised row, both Pockets together", got.Categories)
	}
	if u := got.Categories[0]; u.ID.Valid || u.Name != "" || u.Allocation != nil || u.Spent.String() != "45" {
		t.Errorf("uncategorised row = %+v, want no id, no name, no allocation, 45", u)
	}
	fig := (dashboard.Breakdown{Categories: got.Categories}).CategoriesOnTheWireForTest()
	raw, _ := json.Marshal(fig)
	if string(raw) != `[{"spent":"45.0000"}]` {
		t.Errorf("wire = %s, want the figure alone", raw)
	}
}

func TestFoldBreakdownAllocationOnlyCategoryIsARowAtZeroAndANullAllocationIsNone(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	gym, off := uid(3), uid(4)
	got, err := dashboard.FoldBreakdown(nil,
		[]db.Category{liveCategory(gym, "Gym", false), liveCategory(off, "Off", true)},
		[]db.AllocationsAsOfRow{
			allocationRow(gym, "daily", "0"), // an allocation of zero is an allocation
			{CategoryID: off, Bucket: "daily"},
		}, at)
	if err != nil {
		t.Fatalf("FoldBreakdown: %v", err)
	}
	if len(got.Categories) != 1 {
		t.Fatalf("categories = %+v, want Gym alone: a Category with none has no row without spend", got.Categories)
	}
	g := got.Categories[0]
	if g.Name != "Gym" || g.Active || !g.Spent.IsZero() || g.Allocation == nil || !g.Allocation.IsZero() {
		t.Errorf("row = %+v, want inactive Gym, spent 0, allocation 0", g)
	}
	if len(got.Pockets) != 0 {
		t.Errorf("pockets = %+v, want none: an allocation is not a Pocket's", got.Pockets)
	}
}

func TestFoldBreakdownAnAllocationOfACategoryThatIsNotLiveIsAnError(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	_, err := dashboard.FoldBreakdown(nil, nil, []db.AllocationsAsOfRow{allocationRow(uid(9), "daily", "5")}, at)
	if err == nil {
		t.Error("an allocation of an unknown Category was folded, want an error: the two reads are one snapshot")
	}
}

func TestFoldBreakdownRefusesARowOutsideThePeriodOrOfAnUnknownBucket(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	for _, day := range []string{"2026-10-31", "2026-12-01"} {
		if _, err := dashboard.FoldBreakdown([]db.PeriodSpendRow{spendRow(t, day, uid(1), "Food", true, "daily", uid(10), "Wallet", "1")}, nil, nil, at); err == nil {
			t.Errorf("a row dated %s was folded into the November Period, want an error", day)
		}
	}
	if _, err := dashboard.FoldBreakdown([]db.PeriodSpendRow{spendRow(t, "2026-11-05", uid(1), "Food", true, "other", uid(10), "Wallet", "1")}, nil, nil, at); err == nil {
		t.Error("an unknown bucket was folded, want an error")
	}
}

// The order is spent descending, then id ascending, the uncategorised row after
// the rows of equal spent; a refund-heavy row sinks below zero.
func TestFoldBreakdownOrdersBySpentThenIdAndTheUncategorisedRowLastOnATie(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	a, b, c, d := uid(1), uid(2), uid(3), uid(4)
	p1, p2, p3 := uid(10), uid(11), uid(12)
	// Handed over in an order that is neither by id nor by spend.
	got, err := dashboard.FoldBreakdown([]db.PeriodSpendRow{
		spendRow(t, "2026-11-05", c, "C", true, "daily", p3, "P3", "100"),
		spendRow(t, "2026-11-05", pgtype.UUID{}, "", false, "daily", p2, "P2", "100"),
		spendRow(t, "2026-11-05", a, "A", true, "daily", p1, "P1", "100"),
		spendRow(t, "2026-11-05", d, "D", true, "daily", p1, "P1", "-40"),
		spendRow(t, "2026-11-05", b, "B", true, "daily", p2, "P2", "250"),
	}, nil, nil, at)
	if err != nil {
		t.Fatalf("FoldBreakdown: %v", err)
	}
	var cats []string
	for _, r := range got.Categories {
		if r.ID.Valid {
			cats = append(cats, r.Name)
		} else {
			cats = append(cats, "uncategorised")
		}
	}
	if s := strings.Join(cats, " "); s != "B A C uncategorised D" {
		t.Errorf("categories = %s, want B (250), then the 100s by id (A, C) with the uncategorised row after them, then D (-40)", s)
	}
	var pockets []string
	for _, r := range got.Pockets {
		pockets = append(pockets, r.Name+"="+r.Spent.String())
	}
	if s := strings.Join(pockets, " "); s != "P2=350 P3=100 P1=60" {
		t.Errorf("pockets = %s, want P2=350 P3=100 P1=60", s)
	}
}

func TestFoldBreakdownTiesBreakOnIdWhateverTheOrderTheRowsCameIn(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	ids := []pgtype.UUID{uid(9), uid(3), uid(7), uid(1)}
	var rows []db.PeriodSpendRow
	for _, id := range ids {
		rows = append(rows, spendRow(t, "2026-11-05", id, "X", true, "daily", id, "P", "5"))
	}
	for run := 0; run < 20; run++ { // a map's iteration order is random: so must the answer not be
		got, err := dashboard.FoldBreakdown(rows, nil, nil, at)
		if err != nil {
			t.Fatalf("FoldBreakdown: %v", err)
		}
		for i, want := range []byte{1, 3, 7, 9} {
			if got.Categories[i].ID != uid(want) || got.Pockets[i].ID != uid(want) {
				t.Fatalf("run %d: row %d is %v / %v, want id %d: equal spent is ordered by id ascending", run, i, got.Categories[i].ID.Bytes[15], got.Pockets[i].ID.Bytes[15], want)
			}
		}
	}
}

func TestAnEmptyBreakdownPutsNothingOnTheWire(t *testing.T) {
	var b dashboard.Breakdown
	if b.CategoriesOnTheWireForTest() != nil || b.PocketsOnTheWireForTest() != nil {
		t.Error("an empty list was put on the wire: it is omitted, never [] and never null")
	}
}
