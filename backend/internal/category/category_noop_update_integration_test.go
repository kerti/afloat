package category_test

import (
	"testing"

	"github.com/kerti/afloat/backend/internal/api"
)

// #137: updated_at moves only when a column's value does. The row is backdated,
// so "unchanged" cannot pass by the clock not having ticked.
func TestAnUpdateThatChangesNothingLeavesUpdatedAtAlone(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.member(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "Rent", api.Commitment, ptr("3000000")))
	f.exec(t, `UPDATE categories SET updated_at = '2020-01-01 00:00:00+00' WHERE id = $1`, id)
	stored := func() string {
		return f.scalar(t, `SELECT updated_at::text FROM categories WHERE id = $1`, id)
	}
	want := stored()

	assertUpdated(t, f.update(ctx, t, id, nil, nil))
	if got := stored(); got != want {
		t.Errorf("an empty PATCH moved updated_at: %s, want %s", got, want)
	}
	got := assertUpdated(t, f.update(ctx, t, id, ptr("Rent"), ptr(true)))
	if got.CategoryDisplayName != "Rent" || !got.Active {
		t.Errorf("resend answered %+v, want the row", got)
	}
	if now := stored(); now != want {
		t.Errorf("a PATCH of the stored values moved updated_at: %s, want %s", now, want)
	}

	assertUpdated(t, f.update(ctx, t, id, ptr("Housing"), nil))
	if now := stored(); now == want {
		t.Errorf("a real change left updated_at at %s", now)
	}
}
