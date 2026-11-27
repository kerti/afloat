package dashboard

import (
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/period"
	"github.com/kerti/afloat/backend/internal/trajectory"
)

// ChartPoints is PRD D5's cumulative chart: one point for each day of the
// Period, the target line against the actual line, each read at the end of the
// day (#155 T8). It is made of what the trajectory is made of and of nothing
// else, so the chart and the pace cannot disagree.
//
// The target is T(j), the line's own half-up value, never re-derived here: the
// pace's T(e) is the point before Today's. The actual is the running sum of the
// daily bucket's per-day totals (Spend.ByDay, never the breakdown rows, which
// count a Commitment's Expense under its Pocket), on the days up to Today and
// not after, so a future-dated Expense is on no point before its date and a
// future-dated refund lowers none (CONTEXT.md §Future-dated Expenses). Today's
// point is the one in progress.
func ChartPoints(line trajectory.TargetLine, p period.Period, today period.Date, byDay map[period.Date]decimal.Decimal) []api.ChartPoint {
	days := p.Days()
	points := make([]api.ChartPoint, 0, days)
	cumulative := decimal.Zero
	for j := 1; j <= days; j++ {
		day := p.StartsOn.AddDays(j - 1)
		point := api.ChartPoint{
			Date:   openapi_types.Date{Time: day.Time()},
			Target: money(line.At(j)),
		}
		if !today.Before(day) {
			cumulative = cumulative.Add(byDay[day])
			actual := money(cumulative)
			point.Actual = &actual
		}
		if day == today {
			inProgress := true
			point.InProgress = &inProgress
		}
		points = append(points, point)
	}
	return points
}
