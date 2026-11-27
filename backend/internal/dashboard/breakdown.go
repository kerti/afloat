package dashboard

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/budget"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/period"
)

// CategoryRow is one row of the Daily Categories' breakdown (PRD D7, #155 T10):
// a live Daily Category, or the one row for the spending no Category classifies
// (ID not Valid, no name). Spent and Allocation are all it says: nothing is
// derived from the pair (R15).
type CategoryRow struct {
	ID     pgtype.UUID
	Name   string
	Active bool
	Spent  decimal.Decimal
	// Allocation is the allocation in force Today; nil where there is none, and
	// always nil on the uncategorised row.
	Allocation *decimal.Decimal
}

// PocketRow is one row of the Pockets' breakdown: where the money went, the
// Expenses of both buckets together.
type PocketRow struct {
	ID     pgtype.UUID
	Name   string
	Active bool
	Spent  decimal.Decimal
}

// Breakdown is D7's two lists, in the order they go on the wire.
type Breakdown struct {
	Categories []CategoryRow
	Pockets    []PocketRow
}

// FoldBreakdown works the two lists out of PeriodSpend's rows, the Household's
// live Categories (the names of an allocation with no spending) and the
// allocations in force Today. It reads no statement of its own: the Expenses
// are the ones the trajectory folded, so the breakdowns and the hero cannot
// disagree about one of them.
//
// What counts is the actual line's rule: a live Expense dated in the Period and
// on or before Today, so a future-dated Expense is in neither list (and a
// future-dated refund does not lower one). Only occurred_on decides.
//
// categories holds the live Categories, so a deleted one is not in it, and
// AllocationsAsOf leaves a deleted Category's allocation out. A deleted
// Category or Pocket never has an Expense to show: it cannot be deleted while
// any Expense, live or deleted, references it (CATEGORY_IN_USE, POCKET_IN_USE).
// Inactive ones are in, with their flag: they still hold what they were
// spent on.
func FoldBreakdown(rows []db.PeriodSpendRow, categories []db.Category, allocations []db.AllocationsAsOfRow, at budget.Position) (Breakdown, error) {
	byCategory := map[pgtype.UUID]*CategoryRow{}
	var uncategorised *CategoryRow
	byPocket := map[pgtype.UUID]*PocketRow{}

	for _, row := range rows {
		day := period.DateOf(row.OccurredOn.Time)
		if !at.Period.Contains(day) {
			return Breakdown{}, fmt.Errorf("period spend: a row dated %s is outside the Period %s..%s", day, at.Period.StartsOn, at.Period.EndsOn)
		}
		if at.Today.Before(day) {
			continue
		}
		p, ok := byPocket[row.PocketID]
		if !ok {
			p = &PocketRow{ID: row.PocketID, Name: row.PocketDisplayName, Active: row.PocketActive}
			byPocket[row.PocketID] = p
		}
		p.Spent = p.Spent.Add(row.Amount)

		switch api.Bucket(row.Bucket) {
		case api.Commitment:
			continue
		case api.Daily:
		default:
			return Breakdown{}, fmt.Errorf("period spend: a Category has the bucket %q", row.Bucket)
		}
		if !row.CategoryID.Valid {
			if uncategorised == nil {
				uncategorised = &CategoryRow{}
			}
			uncategorised.Spent = uncategorised.Spent.Add(row.Amount)
			continue
		}
		c, ok := byCategory[row.CategoryID]
		if !ok {
			c = &CategoryRow{ID: row.CategoryID, Name: deref(row.CategoryDisplayName), Active: deref(row.CategoryActive)}
			byCategory[row.CategoryID] = c
		}
		c.Spent = c.Spent.Add(row.Amount)
	}

	live := make(map[pgtype.UUID]db.Category, len(categories))
	for _, c := range categories {
		live[c.ID] = c
	}
	for _, a := range allocations {
		if api.Bucket(a.Bucket) != api.Daily || !a.Allocation.Valid {
			continue
		}
		c, ok := byCategory[a.CategoryID]
		if !ok {
			// An allocation and no spending: a row of its own. Both reads are
			// of the one snapshot, so the Category is in the live list.
			category, found := live[a.CategoryID]
			if !found {
				return Breakdown{}, fmt.Errorf("an allocation names a Category that is not live")
			}
			c = &CategoryRow{ID: category.ID, Name: category.CategoryDisplayName, Active: category.Active}
			byCategory[a.CategoryID] = c
		}
		amount := a.Allocation.Decimal
		c.Allocation = &amount
	}

	var out Breakdown
	for _, c := range byCategory {
		out.Categories = append(out.Categories, *c)
	}
	if uncategorised != nil {
		out.Categories = append(out.Categories, *uncategorised)
	}
	for _, p := range byPocket {
		out.Pockets = append(out.Pockets, *p)
	}
	slices.SortFunc(out.Categories, func(a, b CategoryRow) int {
		return orderBySpentThenID(a.Spent, b.Spent, a.ID, b.ID)
	})
	slices.SortFunc(out.Pockets, func(a, b PocketRow) int {
		return orderBySpentThenID(a.Spent, b.Spent, a.ID, b.ID)
	})
	return out, nil
}

// orderBySpentThenID is the breakdowns' order, declared on the server (R15):
// spent descending, then id ascending, which for UUIDv7 is oldest first. The
// uncategorised row has no id and goes after the rows of equal spent.
func orderBySpentThenID(spentA, spentB decimal.Decimal, idA, idB pgtype.UUID) int {
	if c := spentB.Cmp(spentA); c != 0 {
		return c
	}
	switch {
	case idA.Valid && !idB.Valid:
		return -1
	case !idA.Valid && idB.Valid:
		return 1
	}
	return bytes.Compare(idA.Bytes[:], idB.Bytes[:])
}

// categoriesOnTheWire is the Daily Categories' list, nil (so omitted) when it
// has no row.
func (b Breakdown) categoriesOnTheWire() *[]api.CategoryFigure {
	if len(b.Categories) == 0 {
		return nil
	}
	out := make([]api.CategoryFigure, 0, len(b.Categories))
	for _, c := range b.Categories {
		fig := api.CategoryFigure{Spent: money(c.Spent)}
		if c.ID.Valid {
			id, name, active := openapi_types.UUID(c.ID.Bytes), c.Name, c.Active
			fig.CategoryId, fig.CategoryDisplayName, fig.Active = &id, &name, &active
		}
		if c.Allocation != nil {
			a := money(*c.Allocation)
			fig.Allocation = &a
		}
		out = append(out, fig)
	}
	return &out
}

// pocketsOnTheWire is the Pockets' list, nil (so omitted) when it has no row.
func (b Breakdown) pocketsOnTheWire() *[]api.PocketFigure {
	if len(b.Pockets) == 0 {
		return nil
	}
	out := make([]api.PocketFigure, 0, len(b.Pockets))
	for _, p := range b.Pockets {
		out = append(out, api.PocketFigure{
			PocketId:          openapi_types.UUID(p.ID.Bytes),
			PocketDisplayName: p.Name,
			Active:            p.Active,
			Spent:             money(p.Spent),
		})
	}
	return &out
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
