package budget

import "github.com/shopspring/decimal"

// Figures are the amounts derived from a Budget (CONTEXT.md §Money and
// buckets), computed on the server and sent as they are: exact, with no
// rounding anywhere (PRD N5a). A Daily Pool or Unallocated below zero is
// information, never an error (PRD S9), and is not clamped.
type Figures struct {
	HasMonthlyBudget  bool
	MonthlyBudget     decimal.Decimal
	CommitmentsBudget decimal.Decimal
	// DailyPool and Unallocated are meaningful only when HasMonthlyBudget.
	DailyPool   decimal.Decimal
	Unallocated decimal.Decimal
}

// ComputeFigures applies the arithmetic: Daily Pool is the Monthly Budget less
// the Commitments Budget, and Unallocated is the Daily Pool less the allocations
// of Daily Categories. commitments and dailyAllocated are those two sums.
func ComputeFigures(monthly decimal.Decimal, has bool, commitments, dailyAllocated decimal.Decimal) Figures {
	f := Figures{HasMonthlyBudget: has, CommitmentsBudget: commitments}
	if !has {
		return f
	}
	f.MonthlyBudget = monthly
	f.DailyPool = monthly.Sub(commitments)
	f.Unallocated = f.DailyPool.Sub(dailyAllocated)
	return f
}
