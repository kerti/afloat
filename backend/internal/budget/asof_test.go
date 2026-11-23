package budget_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/period"
)

func day(t *testing.T, s string) period.Date {
	t.Helper()
	d, err := period.ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", s, err)
	}
	return d
}

func version(t *testing.T, on, amount string) budget.Version {
	t.Helper()
	return budget.Version{EffectiveOn: day(t, on), MonthlyBudget: decimal.RequireFromString(amount)}
}

// The as-of rule (#130 C4): the version with the greatest effective_on on or
// before the day, and a day before the first version reads as of the first.
func TestMonthlyBudgetAsOf(t *testing.T) {
	for _, tc := range []struct {
		name     string
		versions func(*testing.T) []budget.Version
		day      string
		want     string // "" is no Budget in force
	}{
		{"no version, no Budget", func(*testing.T) []budget.Version { return nil }, "2026-11-14", ""},
		{"a Budget set on day 14 is read from day 1", func(t *testing.T) []budget.Version {
			return []budget.Version{version(t, "2026-11-14", "9000000")}
		}, "2026-11-01", "9000000"},
		{"and on the day before it was set", func(t *testing.T) []budget.Version {
			return []budget.Version{version(t, "2026-11-14", "9000000")}
		}, "2026-11-13", "9000000"},
		{"and on the day it was set", func(t *testing.T) []budget.Version {
			return []budget.Version{version(t, "2026-11-14", "9000000")}
		}, "2026-11-14", "9000000"},
		{"and for the rest of the Period", func(t *testing.T) []budget.Version {
			return []budget.Version{version(t, "2026-11-14", "9000000")}
		}, "2026-11-30", "9000000"},
		{"a later change does not redraw the days before it", func(t *testing.T) []budget.Version {
			return []budget.Version{version(t, "2026-11-01", "6000000"), version(t, "2026-11-11", "7500000")}
		}, "2026-11-10", "6000000"},
		{"a change is in force on the day it takes effect", func(t *testing.T) []budget.Version {
			return []budget.Version{version(t, "2026-11-01", "6000000"), version(t, "2026-11-11", "7500000")}
		}, "2026-11-11", "7500000"},
		{"and after it", func(t *testing.T) []budget.Version {
			return []budget.Version{version(t, "2026-11-01", "6000000"), version(t, "2026-11-11", "7500000")}
		}, "2026-11-30", "7500000"},
		{"the latest of several versions wins, whatever order they arrive in", func(t *testing.T) []budget.Version {
			return []budget.Version{version(t, "2026-11-20", "3"), version(t, "2026-11-01", "1"), version(t, "2026-11-11", "2")}
		}, "2026-11-25", "3"},
		{"a day between versions takes the earlier one, in any order", func(t *testing.T) []budget.Version {
			return []budget.Version{version(t, "2026-11-20", "3"), version(t, "2026-11-01", "1"), version(t, "2026-11-11", "2")}
		}, "2026-11-15", "2"},
		{"before the first of several, the first", func(t *testing.T) []budget.Version {
			return []budget.Version{version(t, "2026-11-20", "3"), version(t, "2026-11-12", "1")}
		}, "2026-11-03", "1"},
		{"a day after the Period's last version day, as before", func(t *testing.T) []budget.Version {
			return []budget.Version{version(t, "2026-11-01", "0")}
		}, "2026-12-15", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, has := budget.MonthlyBudgetAsOf(tc.versions(t), day(t, tc.day))
			if tc.want == "" {
				if has {
					t.Fatalf("got %s, want no Budget in force", got)
				}
				return
			}
			if !has || !got.Equal(decimal.RequireFromString(tc.want)) {
				t.Errorf("got %s (has %v), want %s", got, has, tc.want)
			}
		})
	}
}

func TestMonthlyBudgetAsOfDoesNotReorderItsInput(t *testing.T) {
	versions := []budget.Version{version(t, "2026-11-20", "3"), version(t, "2026-11-01", "1")}
	budget.MonthlyBudgetAsOf(versions, day(t, "2026-11-25"))
	if versions[0].EffectiveOn != day(t, "2026-11-20") {
		t.Error("the caller's slice was reordered")
	}
}

// S9: Monthly Budget is authoritative, allocations are advisory; the pool and
// Unallocated can be negative and are sent as such.
func TestComputeFigures(t *testing.T) {
	d := decimal.RequireFromString
	for _, tc := range []struct {
		name                                 string
		monthly                              string
		has                                  bool
		commitments, daily                   string
		wantPool, wantUnallocated, wantMonth string
	}{
		{"CONTEXT's worked case: 9.000.000 with 3.000.000 rent", "9000000", true, "3000000", "0", "6000000", "6000000", "9000000"},
		{"no Categories at all: the pool is the Budget", "9000000", true, "0", "0", "9000000", "9000000", "9000000"},
		{"Daily allocations move only Unallocated", "9000000", true, "3000000", "2000000", "6000000", "4000000", "9000000"},
		{"over-allocated: Unallocated is negative and not an error", "9000000", true, "3000000", "7000000", "6000000", "-1000000", "9000000"},
		{"Commitments above the Budget: the pool is negative", "2000000", true, "3000000", "0", "-1000000", "-1000000", "2000000"},
		{"a zero Budget", "0", true, "0", "0", "0", "0", "0"},
		{"no Monthly Budget yet", "0", false, "0", "0", "", "", ""},
		{"exact at four decimals", "100.0001", true, "0.0001", "0.0002", "100.0000", "99.9998", "100.0001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := budget.ComputeFigures(d(tc.monthly), tc.has, d(tc.commitments), d(tc.daily))
			if f.HasMonthlyBudget != tc.has {
				t.Fatalf("HasMonthlyBudget = %v, want %v", f.HasMonthlyBudget, tc.has)
			}
			if !f.CommitmentsBudget.Equal(d(tc.commitments)) {
				t.Errorf("CommitmentsBudget = %s, want %s", f.CommitmentsBudget, tc.commitments)
			}
			if !tc.has {
				return
			}
			if !f.MonthlyBudget.Equal(d(tc.wantMonth)) || !f.DailyPool.Equal(d(tc.wantPool)) || !f.Unallocated.Equal(d(tc.wantUnallocated)) {
				t.Errorf("monthly %s, pool %s, unallocated %s; want %s, %s, %s", f.MonthlyBudget, f.DailyPool, f.Unallocated, tc.wantMonth, tc.wantPool, tc.wantUnallocated)
			}
		})
	}
}
