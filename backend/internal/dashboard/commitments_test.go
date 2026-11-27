package dashboard_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/dashboard"
	"github.com/kerti/afloat/backend/internal/db"
)

// The Commitments fold on rows built by hand (PRD D6, #155 T9, R16): no
// database, so a case that would be awkward to seed is exact.

func commitmentCategory(id pgtype.UUID, name string, active bool) db.Category {
	return db.Category{ID: id, CategoryDisplayName: name, Bucket: "commitment", Active: active}
}

func TestFoldCommitmentsReadsAllocationPaidAndSignedStillDue(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	rent, tax, wallet := uid(1), uid(2), uid(10)
	got, err := dashboard.FoldCommitments([]db.PeriodSpendRow{
		spendRow(t, "2026-11-02", rent, "Rent", true, "commitment", wallet, "Wallet", "1000"),
		spendRow(t, "2026-11-09", rent, "Rent", true, "commitment", wallet, "Wallet", "500"),
		spendRow(t, "2026-11-10", tax, "Tax", true, "commitment", wallet, "Wallet", "1200"), // more than set aside
	}, []db.Category{commitmentCategory(rent, "Rent", true), commitmentCategory(tax, "Tax", true)},
		[]db.AllocationsAsOfRow{allocationRow(rent, "commitment", "2000"), allocationRow(tax, "commitment", "1000")}, at)
	if err != nil {
		t.Fatalf("FoldCommitments: %v", err)
	}
	want := map[pgtype.UUID][3]string{rent: {"2000", "1500", "500"}, tax: {"1000", "1200", "-200"}}
	if len(got.Items) != 2 {
		t.Fatalf("items = %+v, want two", got.Items)
	}
	for _, it := range got.Items {
		w := want[it.ID]
		if it.Allocation.String() != w[0] || it.Paid.String() != w[1] || it.StillDue.String() != w[2] {
			t.Errorf("%s = allocation %s paid %s still due %s, want %v", it.Name, it.Allocation, it.Paid, it.StillDue, w)
		}
	}
	// The totals are the rows' sums, and still due is signed per row and in the
	// total: Tax's overpayment is not clamped to zero before it is added.
	if got.Budget.String() != "3000" || got.Paid.String() != "2700" || got.StillDue.String() != "300" {
		t.Errorf("totals = %s / %s / %s, want 3000 / 2700 / 300", got.Budget, got.Paid, got.StillDue)
	}
}

func TestFoldCommitmentsPaidIsNotPaidUntilItsDay(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	rent, wallet := uid(1), uid(10)
	got, err := dashboard.FoldCommitments([]db.PeriodSpendRow{
		spendRow(t, "2026-11-14", rent, "Rent", true, "commitment", wallet, "Wallet", "100"), // Today counts
		spendRow(t, "2026-11-15", rent, "Rent", true, "commitment", wallet, "Wallet", "400"), // tomorrow does not
		spendRow(t, "2026-11-30", rent, "Rent", true, "commitment", wallet, "Wallet", "-50"), // a future refund lowers nothing
	}, []db.Category{commitmentCategory(rent, "Rent", true)}, []db.AllocationsAsOfRow{allocationRow(rent, "commitment", "1000")}, at)
	if err != nil {
		t.Fatalf("FoldCommitments: %v", err)
	}
	if it := got.Items[0]; it.Paid.String() != "100" || it.StillDue.String() != "900" {
		t.Errorf("rent = paid %s still due %s, want 100 and 900", it.Paid, it.StillDue)
	}
}

func TestFoldCommitmentsAZeroStillDueAndARefundAreFigures(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	rent, tax, wallet := uid(1), uid(2), uid(10)
	got, err := dashboard.FoldCommitments([]db.PeriodSpendRow{
		spendRow(t, "2026-11-02", rent, "Rent", true, "commitment", wallet, "Wallet", "1000"),
		spendRow(t, "2026-11-03", tax, "Tax", true, "commitment", wallet, "Wallet", "-300"), // a refund: paid goes negative
	}, []db.Category{commitmentCategory(rent, "Rent", true), commitmentCategory(tax, "Tax", true)},
		[]db.AllocationsAsOfRow{allocationRow(rent, "commitment", "1000"), allocationRow(tax, "commitment", "0")}, at)
	if err != nil {
		t.Fatalf("FoldCommitments: %v", err)
	}
	if it := got.Items[0]; !it.StillDue.IsZero() {
		t.Errorf("rent still due = %s, want zero", it.StillDue)
	}
	if it := got.Items[1]; it.Paid.String() != "-300" || it.StillDue.String() != "300" {
		t.Errorf("tax = paid %s still due %s, want -300 and 300 (an allocation of zero is an allocation)", it.Paid, it.StillDue)
	}
}

func TestFoldCommitmentsListsEveryLiveCommitmentInTheCategoryOrderWithOrWithoutSpend(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	a, b, c, food, wallet := uid(1), uid(2), uid(3), uid(4), uid(10)
	got, err := dashboard.FoldCommitments([]db.PeriodSpendRow{
		spendRow(t, "2026-11-05", food, "Food", true, "daily", wallet, "Wallet", "999"), // a daily Expense is no Commitment's
		spendRow(t, "2026-11-05", pgtype.UUID{}, "", false, "daily", wallet, "Wallet", "1"),
		spendRow(t, "2026-11-06", c, "C", false, "commitment", wallet, "Wallet", "5"),
	}, []db.Category{commitmentCategory(a, "A", true), liveCategory(food, "Food", true), commitmentCategory(b, "B", true), commitmentCategory(c, "C", false)},
		[]db.AllocationsAsOfRow{allocationRow(a, "commitment", "10"), allocationRow(food, "daily", "50"), allocationRow(b, "commitment", "20"), allocationRow(c, "commitment", "30")}, at)
	if err != nil {
		t.Fatalf("FoldCommitments: %v", err)
	}
	var ids []pgtype.UUID
	for _, it := range got.Items {
		ids = append(ids, it.ID)
	}
	if len(ids) != 3 || ids[0] != a || ids[1] != b || ids[2] != c {
		t.Fatalf("items = %v, want A, B, C: the live Commitments in the Category list's order, no Daily Category", ids)
	}
	if got.Items[2].Active {
		t.Errorf("C active = %v, want false: an inactive Commitment is kept, with its flag", got.Items[2].Active)
	}
	if got.Paid.String() != "5" || got.Budget.String() != "60" {
		t.Errorf("totals = budget %s paid %s, want 60 and 5", got.Budget, got.Paid)
	}
}

func TestFoldCommitmentsHasNothingWithoutALiveCommitment(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	food, wallet := uid(1), uid(10)
	got, err := dashboard.FoldCommitments([]db.PeriodSpendRow{
		spendRow(t, "2026-11-05", food, "Food", true, "daily", wallet, "Wallet", "10"),
	}, []db.Category{liveCategory(food, "Food", true)}, []db.AllocationsAsOfRow{allocationRow(food, "daily", "50")}, at)
	if err != nil {
		t.Fatalf("FoldCommitments: %v", err)
	}
	if len(got.Items) != 0 {
		t.Errorf("items = %+v, want none", got.Items)
	}
	if fig := got.OnTheWireForTest(); fig != nil {
		t.Errorf("wire = %+v, want nil so the property is omitted", fig)
	}
}

// A Commitment always has an allocation, but a viewer whose Today is before the
// day it was first set (a member in a zone behind the writer's) has none in
// force: it is read as zero, as the Daily Pool already reads it, and the row is
// not dropped.
func TestFoldCommitmentsReadsACommitmentWithNoAllocationInForceAsZero(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	rent := uid(1)
	got, err := dashboard.FoldCommitments(nil, []db.Category{commitmentCategory(rent, "Rent", true)}, nil, at)
	if err != nil {
		t.Fatalf("FoldCommitments: %v", err)
	}
	if len(got.Items) != 1 || !got.Items[0].Allocation.IsZero() || !got.Items[0].StillDue.IsZero() {
		t.Errorf("items = %+v, want one row at zero", got.Items)
	}
}

func TestFoldCommitmentsRefusesRowsItCannotPlace(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	rent, wallet := uid(1), uid(10)
	live := []db.Category{commitmentCategory(rent, "Rent", true)}
	for name, rows := range map[string][]db.PeriodSpendRow{
		"a Commitment Expense whose Category is not live": {spendRow(t, "2026-11-05", uid(9), "Gone", true, "commitment", wallet, "Wallet", "1")},
		"a row outside the Period":                        {spendRow(t, "2026-12-05", rent, "Rent", true, "commitment", wallet, "Wallet", "1")},
		"a Category of an unknown bucket":                 {spendRow(t, "2026-11-05", rent, "Rent", true, "weekly", wallet, "Wallet", "1")},
	} {
		if _, err := dashboard.FoldCommitments(rows, live, nil, at); err == nil {
			t.Errorf("%s: want an error, not a figure that quietly leaves it out", name)
		}
	}
}

func TestCommitmentsOnTheWireHasFourDecimalsAndKeepsASign(t *testing.T) {
	at := positionOf(t, "2026-11-14")
	tax, wallet := uid(2), uid(10)
	got, err := dashboard.FoldCommitments([]db.PeriodSpendRow{
		spendRow(t, "2026-11-10", tax, "Tax", false, "commitment", wallet, "Wallet", "1200"),
	}, []db.Category{commitmentCategory(tax, "Tax", false)}, []db.AllocationsAsOfRow{allocationRow(tax, "commitment", "1000")}, at)
	if err != nil {
		t.Fatalf("FoldCommitments: %v", err)
	}
	raw, err := json.Marshal(got.OnTheWireForTest())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"commitments_budget":"1000.0000"`, `"paid":"1200.0000"`, `"still_due":"-200.0000"`,
		`"category_display_name":"Tax"`, `"active":false`, `"allocation":"1000.0000"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("wire = %s, want it to contain %s", raw, want)
		}
	}
}
