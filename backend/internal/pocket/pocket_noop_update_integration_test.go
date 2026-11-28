package pocket_test

import (
	"context"
	"testing"
)

// #137: updated_at moves only when a column's value does. The row is backdated,
// so "unchanged" cannot pass by the clock not having ticked.
func TestAnUpdateThatChangesNothingLeavesUpdatedAtAlone(t *testing.T) {
	f := newFixture(t)
	ctx := f.memberWithKeeper(t, "Home", "a@example.com")
	id := newID(t)
	assertCreated(t, f.create(ctx, t, id, "OVO"))
	f.exec(t, `UPDATE pockets SET updated_at = '2020-01-01 00:00:00+00' WHERE id = $1`, id)
	stored := func() string {
		t.Helper()
		var s string
		if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT updated_at::text FROM pockets WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	want := stored()

	assertUpdated(t, f.update(ctx, t, id, nil, nil))
	if got := stored(); got != want {
		t.Errorf("an empty PATCH moved updated_at: %s, want %s", got, want)
	}
	got := assertUpdated(t, f.update(ctx, t, id, ptr("OVO"), ptr(true)))
	if got.PocketDisplayName != "OVO" || !got.Active {
		t.Errorf("resend answered %+v, want the row", got)
	}
	if now := stored(); now != want {
		t.Errorf("a PATCH of the stored values moved updated_at: %s, want %s", now, want)
	}

	assertUpdated(t, f.update(ctx, t, id, ptr("Gopay"), nil))
	if now := stored(); now == want {
		t.Errorf("a real change left updated_at at %s", now)
	}
}
