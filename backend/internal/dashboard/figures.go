package dashboard

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/period"
	"github.com/kerti/afloat/backend/internal/trajectory"
)

// Assembly is everything the trajectory is made of for one Position, and what
// it answered. The answer is the wire's source; the rest is kept so a fixture
// can pin the inputs (the pool through the Period) as well as the outputs.
type Assembly struct {
	// DailyPool is P, the Daily Pool in force Today: the one GET /budget answers
	// as `daily_pool` on the same state.
	DailyPool decimal.Decimal
	// BasePool is the Daily Pool the Period's first day is read as of (a Budget
	// set on day 14 is its Budget from day 1, so it is no change).
	BasePool decimal.Decimal
	// Changes are the days after the first and on or before Today on which the
	// Daily Pool really changed, in order, as the target line takes them.
	Changes []trajectory.PoolChange
	// PeriodDays is D, DaysElapsed is e; r = D - e counts Today.
	PeriodDays, DaysElapsed int
	Spend                   Spend
	Figures                 trajectory.Figures
}

// DaysRemaining is r: the days left counting Today, so the last day reads 1.
func (a Assembly) DaysRemaining() int { return a.PeriodDays - a.DaysElapsed }

// RemainingDailyPool is P' - S_settled (#155 R6): the number the adaptive
// allowance is made of, frozen through Today because today's spending is no
// part of it. Signed; negative once the Available Pool is used up.
func (a Assembly) RemainingDailyPool() decimal.Decimal {
	return a.Figures.AvailablePool.Sub(a.Spend.Settled)
}

// Assemble works the trajectory out for one Position from stored rows: the
// Daily Pool today and through the Period, the Period's spend, and the
// calculation over them (#155 T2, T3). ok is false while the Household has no
// Monthly Budget, which is not an error: there is nothing to read the
// spending against.
//
// It must run inside the dashboard's snapshot, after the Period was ensured,
// and every statement filters on the Household. Today and the Period come from
// the Position, never from a clock or from the Household's current settings, so
// a setting changed between that read and this one cannot tear the answer.
func Assemble(ctx context.Context, q db.Querier, householdID pgtype.UUID, at budget.Position, mode trajectory.Mode) (a Assembly, ok bool, err error) {
	rows, err := q.ListMonthlyBudgetVersions(ctx, db.ListMonthlyBudgetVersionsParams{
		HouseholdID: householdID,
		YearMonth:   at.Period.YearMonth,
	})
	if err != nil {
		return Assembly{}, false, fmt.Errorf("list monthly budget versions: %w", err)
	}
	if len(rows) == 0 {
		return Assembly{}, false, nil
	}
	versions := make([]budget.Version, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, budget.Version{
			EffectiveOn:   period.DateOf(row.EffectiveOn.Time),
			MonthlyBudget: row.MonthlyBudget,
		})
	}

	pools := poolReader{ctx: ctx, q: q, householdID: householdID, yearMonth: at.Period.YearMonth, versions: versions, cache: map[period.Date]decimal.Decimal{}}

	// The pool in force Today, by the same two functions GET /budget uses.
	poolToday, err := pools.at(at.Today)
	if err != nil {
		return Assembly{}, false, err
	}
	// The base pool is the pool as of the Period's first day, so a version a
	// Household wrote on day 14 is its Budget from day 1, with no kink; a
	// version dated before a re-cut window is in force on the first day, the
	// latest winning, because the as-of rule says so.
	basePool, err := pools.at(at.Period.StartsOn)
	if err != nil {
		return Assembly{}, false, err
	}

	days, err := pools.changeDays(at)
	if err != nil {
		return Assembly{}, false, err
	}
	// Only a real change kinks: a day whose pool equals the one before it (a
	// Daily Category's allocation, or an edit back to the same amount) is no
	// change and is left out.
	changes := make([]trajectory.PoolChange, 0, len(days))
	previous := basePool
	for _, day := range days {
		pool, err := pools.at(day)
		if err != nil {
			return Assembly{}, false, err
		}
		if pool.Equal(previous) {
			continue
		}
		previous = pool
		changes = append(changes, trajectory.PoolChange{EffectiveDay: day.Sub(at.Period.StartsOn) + 1, DailyPool: pool})
	}

	periodDays, elapsed := at.Period.Days(), at.Today.Sub(at.Period.StartsOn)
	line, err := trajectory.NewTargetLine(periodDays, basePool, changes)
	if err != nil {
		return Assembly{}, false, fmt.Errorf("target line: %w", err)
	}
	target, baseline := line.AtToday(elapsed)

	spend, err := LoadSpend(ctx, q, householdID, at)
	if err != nil {
		return Assembly{}, false, err
	}
	figures, err := trajectory.Calculate(trajectory.Inputs{
		DailyPool:            poolToday,
		PeriodDays:           periodDays,
		DaysElapsed:          elapsed,
		SettledSpend:         spend.Settled,
		TodaySpend:           spend.Today,
		FutureSpend:          spend.Future,
		Mode:                 mode,
		Parameters:           trajectory.DefaultParameters(),
		TargetThroughElapsed: target,
		BaselineInForce:      baseline,
	})
	if err != nil {
		return Assembly{}, false, fmt.Errorf("calculate: %w", err)
	}
	return Assembly{
		DailyPool: poolToday, BasePool: basePool, Changes: changes,
		PeriodDays: periodDays, DaysElapsed: elapsed, Spend: spend, Figures: figures,
	}, true, nil
}

// poolReader reads the Daily Pool as of a Period Day from the Monthly Budget's
// versions (already read) and the allocations (one primary-key range scan per
// day asked about, the as-of rule staying written once in AllocationsAsOf).
type poolReader struct {
	ctx         context.Context
	q           db.Querier
	householdID pgtype.UUID
	yearMonth   string
	versions    []budget.Version
	cache       map[period.Date]decimal.Decimal
}

// at is the Daily Pool in force on a Period Day: the Monthly Budget less the
// Commitments' allocations, both as of budget.InForceDay, as GET /budget reads
// them. Inactive Categories count; a deleted one's allocation is not there.
func (p poolReader) at(day period.Date) (decimal.Decimal, error) {
	if pool, ok := p.cache[day]; ok {
		return pool, nil
	}
	monthly, has := budget.MonthlyBudgetAsOf(p.versions, day)
	allocated, err := p.q.AllocationsAsOf(p.ctx, db.AllocationsAsOfParams{
		HouseholdID: p.householdID,
		YearMonth:   p.yearMonth,
		AsOf:        pgDate(budget.InForceDay(p.versions, day)),
	})
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("list allocations: %w", err)
	}
	commitments, daily := decimal.Zero, decimal.Zero
	for _, row := range allocated {
		switch api.Bucket(row.Bucket) {
		case api.Commitment:
			commitments = commitments.Add(row.Allocation.Decimal)
		case api.Daily:
			daily = daily.Add(row.Allocation.Decimal)
		default:
			return decimal.Decimal{}, fmt.Errorf("a Category has the bucket %q", row.Bucket)
		}
	}
	pool := budget.ComputeFigures(monthly, has, commitments, daily).DailyPool
	p.cache[day] = pool
	return pool, nil
}

// changeDays are the days a Daily Pool could have changed on: the effective
// dates of the Monthly Budget log and of the allocations, after the Period's
// first day and on or before Today, ascending and distinct. A version dated
// after Today (a member in a zone ahead wrote it) is not in force and never
// kinks the line; one dated on or before the first day is the base. Whether a
// day really changes the pool is the target line's to merge away, so a Daily
// Category's allocation, which leaves the pool alone, costs a statement and no
// kink.
func (p poolReader) changeDays(at budget.Position) ([]period.Date, error) {
	var days []period.Date
	for _, v := range p.versions {
		days = append(days, v.EffectiveOn)
	}
	allocated, err := p.q.ListAllocationChangeDays(p.ctx, db.ListAllocationChangeDaysParams{
		HouseholdID: p.householdID,
		YearMonth:   p.yearMonth,
	})
	if err != nil {
		return nil, fmt.Errorf("list allocation change days: %w", err)
	}
	for _, d := range allocated {
		days = append(days, period.DateOf(d.Time))
	}
	days = slices.DeleteFunc(days, func(d period.Date) bool {
		return !at.Period.StartsOn.Before(d) || at.Today.Before(d)
	})
	slices.SortFunc(days, func(a, b period.Date) int { return a.Sub(b) })
	return slices.Compact(days), nil
}

// trajectoryOnTheWire is the Assembly as #155 renders it (R5): the allowance,
// Left to Spend Today, Days Remaining, the Remaining Daily Pool and the
// Baseline (PR 3), and the State and Trajectory Variance (PR 6). Buffer Days
// is computed and fixture-pinned but never on the wire (R7). Money is exactly
// four decimals, a negative keeps its sign.
//
// The pace is absent while the Household is fully committed (no Pace): no
// baseline, no State, no variance. The State is present on every day the pace
// is, day one included, where it reads a plain afloat (R13); the variance is
// not, because on day one nothing has settled and there is nothing to say
// about the pace (R5, T12). Warm-up pins the State and leaves the variance as
// it is.
func (a Assembly) trajectoryOnTheWire() *api.Trajectory {
	out := &api.Trajectory{
		LeftToSpendToday:   money(a.Figures.LeftToSpendToday),
		TodaysAllowance:    money(a.Figures.TodaysAllowance),
		DaysRemaining:      a.DaysRemaining(),
		RemainingDailyPool: money(a.RemainingDailyPool()),
	}
	if a.Figures.Pace != nil {
		b := money(a.Figures.Pace.BaselineDailyBudget)
		out.BaselineDailyBudget = &b
		state := api.TrajectoryState(a.Figures.Pace.State)
		out.State = &state
		if a.DaysElapsed > 0 {
			v := money(a.Figures.Pace.TrajectoryVariance)
			out.TrajectoryVariance = &v
		}
	}
	return out
}

// money is the wire spelling of an amount: exactly four decimals, never trimmed
// and never an exponent (BOOTSTRAP.md §4). StringFixed, not String, which strips
// trailing zeros.
func money(d decimal.Decimal) api.Money {
	return d.StringFixed(4)
}
