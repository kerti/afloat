package dashboard

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/period"
)

// Spend is what the Period's Expenses add up to against Today, for the daily
// bucket: the only spend the trajectory reads (CONTEXT.md; #140 X16). A
// Commitment Category's Expense is in none of it and an uncategorised one is
// (it is Daily). Every figure is bounded by the current Period: an Expense in
// any other Period counts toward that Period's figures only (#140 R13).
type Spend struct {
	// ByDay is the daily bucket's total per Period Day of the window, for the
	// days that have any. A refund is a negative total.
	ByDay map[period.Date]decimal.Decimal
	// Settled is S_settled: the Period's daily spend before Today.
	Settled decimal.Decimal
	// Today is S_today: the daily spend on Today.
	Today decimal.Decimal
	// Future is S_future: the daily spend after Today and on or before the
	// Period's last day, signed. A future-dated refund makes it smaller, and
	// whether that raises anything is the calculation's to say (R3).
	Future decimal.Decimal
}

// LoadPeriodSpend is the one statement every figure that reads Expenses is made
// of (PeriodSpend), over the Position's Period. It is meant to run in the
// dashboard's snapshot.
func LoadPeriodSpend(ctx context.Context, q db.Querier, householdID pgtype.UUID, at budget.Position) ([]db.PeriodSpendRow, error) {
	rows, err := q.PeriodSpend(ctx, db.PeriodSpendParams{
		HouseholdID: householdID,
		StartsOn:    pgDate(at.Period.StartsOn),
		EndsOn:      pgDate(at.Period.EndsOn),
	})
	if err != nil {
		return nil, fmt.Errorf("period spend: %w", err)
	}
	return rows, nil
}

// LoadSpend reads the Period's spend with that statement and folds it. It is
// meant to run in the dashboard's snapshot, so what it returns and what the
// other figures read agree about every Expense.
func LoadSpend(ctx context.Context, q db.Querier, householdID pgtype.UUID, at budget.Position) (Spend, error) {
	rows, err := LoadPeriodSpend(ctx, q, householdID, at)
	if err != nil {
		return Spend{}, err
	}
	return FoldSpend(rows, at)
}

// FoldSpend folds PeriodSpend's rows into Spend. Only occurred_on decides the
// day and so the Period; captured_at is never read (CLAUDE.md rule 5). The
// statement bounds the window, so a row outside it is a bug in the statement and
// an error here, not a figure that quietly includes it.
func FoldSpend(rows []db.PeriodSpendRow, at budget.Position) (Spend, error) {
	s := Spend{ByDay: map[period.Date]decimal.Decimal{}}
	for _, row := range rows {
		day := period.DateOf(row.OccurredOn.Time)
		if !at.Period.Contains(day) {
			return Spend{}, fmt.Errorf("period spend: a row dated %s is outside the Period %s..%s", day, at.Period.StartsOn, at.Period.EndsOn)
		}
		switch api.Bucket(row.Bucket) {
		case api.Daily:
		case api.Commitment:
			// Reported separately as "paid" (FoldCommitments), and in no daily
			// figure.
			continue
		default:
			// The CHECK on categories.bucket makes this unreachable; failing
			// loudly beats a figure that quietly leaves an Expense out.
			return Spend{}, fmt.Errorf("period spend: a Category has the bucket %q", row.Bucket)
		}
		s.ByDay[day] = s.ByDay[day].Add(row.Amount)
		switch {
		case day.Before(at.Today):
			s.Settled = s.Settled.Add(row.Amount)
		case day == at.Today:
			s.Today = s.Today.Add(row.Amount)
		default:
			s.Future = s.Future.Add(row.Amount)
		}
	}
	return s, nil
}

func pgDate(d period.Date) pgtype.Date {
	return pgtype.Date{Time: d.Time(), Valid: true}
}
