package dashboard

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/period"
)

// CommitmentRow is one live Commitment Category's row of PRD D6: what it is set
// at, what has been paid against it, and the difference.
type CommitmentRow struct {
	ID     pgtype.UUID
	Name   string
	Active bool
	// Allocation is the allocation in force Today.
	Allocation decimal.Decimal
	// Paid is the Category's live Expenses dated in the Period and on or before
	// Today, signed.
	Paid decimal.Decimal
	// StillDue is Allocation - Paid, signed and never clamped (#155 R16).
	StillDue decimal.Decimal
}

// Commitments is D6: the Household's Commitments read per Budget Period, rows
// in the Category list's order and the totals of them. It is empty (no Items)
// while the Household has no live Commitment Category.
type Commitments struct {
	// Budget is the Commitments Budget as in force Today: the rows' allocations.
	Budget, Paid, StillDue decimal.Decimal
	Items                  []CommitmentRow
}

// FoldCommitments works D6 out of PeriodSpend's rows, the Household's live
// Categories and the allocations in force Today. It reads no statement of its
// own: the Expenses are the ones the trajectory folded and the allocations are
// the rows the Daily Pool of Today was made of, so the panel and the pool
// cannot disagree about one of them (R16).
//
// Paid is the actual line's rule: a live Expense dated in the Period and on or
// before Today, so a future-dated one is not paid until its day. Only
// occurred_on decides. A Commitment annual in nature is read as any other, per
// Period; the accepted distortion waits for the plan object (Q-20).
//
// categories is in the Category list's order, which is the order of the rows. A
// Commitment always has an allocation; where a viewer's Today is before the day
// it was first set (a zone behind the writer's) none is in force and it reads
// as zero, as the Daily Pool already reads it, and the row stays.
func FoldCommitments(rows []db.PeriodSpendRow, categories []db.Category, allocations []db.AllocationsAsOfRow, at budget.Position) (Commitments, error) {
	var out Commitments
	index := map[pgtype.UUID]int{}
	for _, c := range categories {
		switch api.Bucket(c.Bucket) {
		case api.Commitment:
			index[c.ID] = len(out.Items)
			out.Items = append(out.Items, CommitmentRow{ID: c.ID, Name: c.CategoryDisplayName, Active: c.Active})
		case api.Daily:
		default:
			return Commitments{}, fmt.Errorf("a Category has the bucket %q", c.Bucket)
		}
	}
	for _, a := range allocations {
		if i, ok := index[a.CategoryID]; ok && a.Allocation.Valid {
			out.Items[i].Allocation = a.Allocation.Decimal
		}
	}
	for _, row := range rows {
		day := period.DateOf(row.OccurredOn.Time)
		if !at.Period.Contains(day) {
			return Commitments{}, fmt.Errorf("period spend: a row dated %s is outside the Period %s..%s", day, at.Period.StartsOn, at.Period.EndsOn)
		}
		switch api.Bucket(row.Bucket) {
		case api.Commitment:
		case api.Daily:
			continue
		default:
			return Commitments{}, fmt.Errorf("period spend: a Category has the bucket %q", row.Bucket)
		}
		if at.Today.Before(day) {
			continue
		}
		// A Category cannot be deleted while any Expense references it, and both
		// reads are of the one snapshot, so a Commitment Expense names a live one.
		i, ok := index[row.CategoryID]
		if !ok {
			return Commitments{}, fmt.Errorf("period spend: a Commitment Expense names a Category that is not live")
		}
		out.Items[i].Paid = out.Items[i].Paid.Add(row.Amount)
	}
	for i := range out.Items {
		it := &out.Items[i]
		it.StillDue = it.Allocation.Sub(it.Paid)
		out.Budget = out.Budget.Add(it.Allocation)
		out.Paid = out.Paid.Add(it.Paid)
	}
	out.StillDue = out.Budget.Sub(out.Paid)
	return out, nil
}

// onTheWire is the panel, nil (so omitted) while the Household has no live
// Commitment Category.
func (c Commitments) onTheWire() *api.Commitments {
	if len(c.Items) == 0 {
		return nil
	}
	items := make([]api.CommitmentFigure, 0, len(c.Items))
	for _, it := range c.Items {
		items = append(items, api.CommitmentFigure{
			CategoryId:          openapi_types.UUID(it.ID.Bytes),
			CategoryDisplayName: it.Name,
			Active:              it.Active,
			Allocation:          money(it.Allocation),
			Paid:                money(it.Paid),
			StillDue:            money(it.StillDue),
		})
	}
	return &api.Commitments{
		CommitmentsBudget: money(c.Budget),
		Paid:              money(c.Paid),
		StillDue:          money(c.StillDue),
		Items:             items,
	}
}
