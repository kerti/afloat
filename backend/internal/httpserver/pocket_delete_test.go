package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// DELETE /pockets/{id} and POST /pockets/{id}/restore through the whole stack:
// the cross-site guard, the session check, the binder and the handler, over a
// real database (#121, PR 3).

func (f pocketFixture) del(id string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.doAt(http.MethodDelete, "/api/pockets/"+id, "", mutate...)
}

func (f pocketFixture) restore(id string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.doAt(http.MethodPost, "/api/pockets/"+id+"/restore", "", mutate...)
}

func (f pocketFixture) deletedAt(t *testing.T, id string) (set bool) {
	t.Helper()
	if err := f.tdb.Pool.QueryRow(context.Background(),
		`SELECT deleted_at IS NOT NULL FROM pockets WHERE id = $1`, id).Scan(&set); err != nil {
		t.Fatalf("read pocket: %v", err)
	}
	return set
}

func TestDeleteAndRestoreAPocketThroughTheRouter(t *testing.T) {
	f := newPocketFixture(t)
	f.seed(t, pocketV7, "OVO")
	f.seed(t, pocketV7b, "Zeta")
	if rec := f.patch(pocketV7, `{"active":false}`); rec.Code != http.StatusOK {
		t.Fatalf("deactivate = %d", rec.Code)
	}

	rec := f.del(pocketV7)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("DELETE = %d %q, want 204 with no body", rec.Code, rec.Body.String())
	}
	if !f.deletedAt(t, pocketV7) {
		t.Error("the row has no deleted_at")
	}
	if n := f.count(t); n != 2 {
		t.Errorf("pockets = %d, want 2: a delete removes no row", n)
	}
	if got, want := body(f.do(http.MethodGet, "")), `{"items":[{"active":true,"id":"`+pocketV7b+`","pocket_display_name":"Zeta"}]}`; got != want {
		t.Errorf("list = %s, want %s", got, want)
	}
	if rec := f.patch(pocketV7, `{"active":true}`); rec.Code != http.StatusNotFound || body(rec) != `{"code":"NOT_FOUND"}` {
		t.Errorf("PATCH on a deleted Pocket = %d %s, want 404 NOT_FOUND", rec.Code, body(rec))
	}

	// Idempotent, and the repeat leaves the row exactly as the first left it.
	first := f.row(t, pocketV7)
	if rec := f.del(pocketV7); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Errorf("a repeat DELETE = %d %q, want 204", rec.Code, rec.Body.String())
	}
	if after := f.row(t, pocketV7); after != first {
		t.Errorf("a repeat DELETE changed the row:\n%s\n%s", first, after)
	}

	// Restore: the Pocket as it was, still inactive; idempotent.
	want := `{"active":false,"id":"` + pocketV7 + `","pocket_display_name":"OVO"}`
	for _, label := range []string{"restore", "a repeat restore"} {
		rec := f.restore(pocketV7)
		if rec.Code != http.StatusOK || body(rec) != want {
			t.Errorf("%s = %d %s, want 200 %s", label, rec.Code, body(rec), want)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s Content-Type = %q", label, ct)
		}
	}
	if f.deletedAt(t, pocketV7) {
		t.Error("the row is still deleted")
	}
}

// The name was released by the delete; a restore into a name that was taken
// since is 409 NAME_TAKEN, and the Pocket stays deleted.
func TestRestoreIntoATakenNameIs409ThroughTheRouter(t *testing.T) {
	f := newPocketFixture(t)
	f.seed(t, pocketV7, "OVO")
	if rec := f.del(pocketV7); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", rec.Code)
	}
	f.seed(t, pocketV7b, "ovo")
	before := f.row(t, pocketV7)

	rec := f.restore(pocketV7)
	if rec.Code != http.StatusConflict || body(rec) != `{"code":"NAME_TAKEN"}` {
		t.Errorf("restore = %d %s, want 409 NAME_TAKEN", rec.Code, body(rec))
	}
	if after := f.row(t, pocketV7); after != before {
		t.Errorf("a refused restore changed the row:\n%s\n%s", before, after)
	}
}

// Unknown and foreign ids are one answer, for both operations, byte for byte
// (and the same bytes PATCH gives); another Household's rows do not change.
func TestDeleteAndRestoreNotFoundIsOneAnswer(t *testing.T) {
	f := newPocketFixture(t)
	other := f.tdb.CreateHousehold(t, "Other")
	f.tdb.CreatePocket(t, other, "Theirs")
	var theirs string
	if err := f.tdb.Pool.QueryRow(context.Background(), `SELECT id::text FROM pockets WHERE household_id = $1`, other).Scan(&theirs); err != nil {
		t.Fatalf("read: %v", err)
	}
	before := f.row(t, theirs)
	f.seed(t, pocketV7, "Mine")

	unknown := "0192f0a0-0000-7000-8000-0000000000ff"
	for name, call := range map[string]func(string) *httptest.ResponseRecorder{
		"DELETE":  func(id string) *httptest.ResponseRecorder { return f.del(id) },
		"restore": func(id string) *httptest.ResponseRecorder { return f.restore(id) },
	} {
		t.Run(name, func(t *testing.T) {
			ref := call(unknown)
			if ref.Code != http.StatusNotFound || body(ref) != `{"code":"NOT_FOUND"}` {
				t.Fatalf("unknown id = %d %s, want 404 NOT_FOUND", ref.Code, body(ref))
			}
			for _, id := range []string{theirs, strings.ToUpper(theirs), "urn:uuid:" + theirs} {
				rec := call(id)
				if rec.Code != ref.Code || rec.Body.String() != ref.Body.String() || rec.Header().Get("Content-Type") != ref.Header().Get("Content-Type") {
					t.Errorf("%s = %d %s, want exactly the unknown id's %d %s", id, rec.Code, body(rec), ref.Code, body(ref))
				}
			}
		})
	}
	if after := f.row(t, theirs); after != before {
		t.Errorf("the other Household's row changed:\n%s\n%s", before, after)
	}
	// The same 404 as PATCH: one bytes for every {id} write.
	patch := f.patch(unknown, `{}`)
	if rec := f.del(unknown); rec.Body.String() != patch.Body.String() {
		t.Errorf("DELETE 404 = %q, PATCH 404 = %q", rec.Body.String(), patch.Body.String())
	}
}

// Both are protected before anything else about the request is read.
func TestDeleteAndRestoreWithoutASessionAre401(t *testing.T) {
	f := newPocketFixture(t)
	f.seed(t, pocketV7, "OVO")
	before := f.row(t, pocketV7)
	noCookie := func(r *http.Request) { r.Header.Del("Cookie") }

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"DELETE":                    f.del(pocketV7, noCookie),
		"DELETE, a bad id":          f.del("not-a-uuid", noCookie),
		"DELETE, an unknown id":     f.del("0192f0a0-0000-7000-8000-0000000000ff", noCookie),
		"restore":                   f.restore(pocketV7, noCookie),
		"restore, a bad id":         f.restore("not-a-uuid", noCookie),
		"restore, an unknown id":    f.restore("0192f0a0-0000-7000-8000-0000000000ff", noCookie),
		"restore, a malformed path": f.restore("%2541", noCookie),
		"restore, an empty id":      f.restore("", noCookie),
	} {
		if rec.Code != http.StatusUnauthorized || body(rec) != `{"code":"UNAUTHORIZED"}` {
			t.Errorf("%s = %d %s, want 401 UNAUTHORIZED", name, rec.Code, body(rec))
		}
	}
	if after := f.row(t, pocketV7); after != before {
		t.Errorf("an unauthenticated request changed the row:\n%s\n%s", before, after)
	}
}

// The repo's first DELETE: the cross-site guard covers every non-safe method,
// and answers before the session, the binder and the handler, so a refused
// request deletes nothing and an id that would not bind is refused first.
func TestCrossSiteDeleteAndRestoreAreBlockedAndChangeNothing(t *testing.T) {
	f := newPocketFixture(t)
	f.seed(t, pocketV7, "OVO")
	f.seed(t, pocketV7b, "Gone")
	if rec := f.del(pocketV7b); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", rec.Code)
	}
	liveBefore, goneBefore := f.row(t, pocketV7), f.row(t, pocketV7b)

	cross := func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }
	foreignOrigin := func(r *http.Request) {
		r.Header.Del("Sec-Fetch-Site")
		r.Host = "afloat.test"
		r.Header.Set("Origin", "http://evil.example")
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"DELETE, cross-site":                 f.del(pocketV7, cross),
		"DELETE, a foreign Origin":           f.del(pocketV7, foreignOrigin),
		"DELETE, cross-site, a bad id":       f.del("not-a-uuid", cross),
		"restore, cross-site":                f.restore(pocketV7b, cross),
		"restore, a foreign Origin":          f.restore(pocketV7b, foreignOrigin),
		"restore, cross-site, a bad id":      f.restore("not-a-uuid", cross),
		"DELETE, cross-site, no session":     f.del(pocketV7, cross, func(r *http.Request) { r.Header.Del("Cookie") }),
		"restore, cross-site, no session":    f.restore(pocketV7b, cross, func(r *http.Request) { r.Header.Del("Cookie") }),
		"DELETE, cross-site, unknown id":     f.del("0192f0a0-0000-7000-8000-0000000000ff", cross),
		"restore, cross-site, an unknown id": f.restore("0192f0a0-0000-7000-8000-0000000000ff", cross),
	} {
		if rec.Code != http.StatusForbidden || body(rec) != `{"code":"CROSS_SITE_REQUEST_BLOCKED"}` {
			t.Errorf("%s = %d %s, want 403 CROSS_SITE_REQUEST_BLOCKED", name, rec.Code, body(rec))
		}
	}
	if f.row(t, pocketV7) != liveBefore || f.row(t, pocketV7b) != goneBefore {
		t.Error("a blocked request changed a row")
	}
}

// #106 item 1 against DELETE, the mutating verb a mis-decoded id would hurt
// (PATCH has the same table in pocket_update_test.go): every shape of a path
// parameter through the real router and the generated binder. Every refusal
// leaves the Pocket live; every accepted spelling names the same Pocket and
// deletes it.
func TestDeletePocketPathParameterShapes(t *testing.T) {
	const invalid = `{"code":"VALIDATION","args":{"field":"id","rule":"invalid"}}`

	for _, tc := range []struct {
		name, target string
		method       string
		status       int
		body         string // empty: assert nothing about it
		deleted      bool
	}{
		{"%2541 binds %41", "/api/pockets/%2541", "DELETE", 400, invalid, false},
		{"%25 binds %", "/api/pockets/%25", "DELETE", 400, invalid, false},
		{"100%25 binds 100%", "/api/pockets/100%25", "DELETE", 400, invalid, false},
		{"%2F inside the id", "/api/pockets/" + pocketV7 + "%2F", "DELETE", 400, invalid, false},
		{"%2f lower case", "/api/pockets/a%2fb", "DELETE", 400, invalid, false},
		{"a semicolon", "/api/pockets/a;b", "DELETE", 400, invalid, false},
		{"%3B", "/api/pockets/a%3Bb", "DELETE", 400, invalid, false},
		{"%2e%2e", "/api/pockets/%2e%2e", "DELETE", 400, invalid, false},
		{"%5C", "/api/pockets/a%5Cb", "DELETE", 400, invalid, false},
		{"%00", "/api/pockets/a%00b", "DELETE", 400, invalid, false},
		{"not a uuid", "/api/pockets/not-a-uuid", "DELETE", 400, invalid, false},
		{"one digit short", "/api/pockets/" + pocketV7[:len(pocketV7)-1], "DELETE", 400, invalid, false},

		{"a trailing slash", "/api/pockets/" + pocketV7 + "/", "DELETE", 404, "404 page not found", false},
		{"an empty parameter", "/api/pockets/", "DELETE", 404, "404 page not found", false},

		{"HEAD", "/api/pockets/" + pocketV7, "HEAD", 405, "", false},

		{"upper case", "/api/pockets/" + strings.ToUpper(pocketV7), "DELETE", 204, "", true},
		{"percent-encoded first digit", "/api/pockets/%30192f0a0-0000-7000-8000-000000000001", "DELETE", 204, "", true},
		{"urn:uuid: prefixed", "/api/pockets/urn:uuid:" + pocketV7, "DELETE", 204, "", true},
		{"braced", "/api/pockets/%7B" + pocketV7 + "%7D", "DELETE", 204, "", true},
		{"32 hex digits", "/api/pockets/" + strings.ReplaceAll(pocketV7, "-", ""), "DELETE", 204, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPocketFixture(t)
			f.seed(t, pocketV7, "OVO")
			before := f.row(t, pocketV7)

			rec := f.doAt(tc.method, tc.target, "")
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, body(rec))
			}
			if tc.body != "" && body(rec) != tc.body {
				t.Errorf("body = %s, want %s", body(rec), tc.body)
			}
			if got := f.deletedAt(t, pocketV7); got != tc.deleted {
				t.Errorf("deleted = %v, want %v (the id was %sbound as the Pocket's)", got, tc.deleted, map[bool]string{true: "not ", false: ""}[tc.deleted])
			}
			if !tc.deleted {
				if after := f.row(t, pocketV7); after != before {
					t.Errorf("the row changed:\n%s\n%s", before, after)
				}
			}
		})
	}
}

// The restore route has the same binder: a malformed id is the same 400 (an
// empty one included), a spelling uuid.Parse accepts is the same Pocket, and a
// trailing slash matches no route.
func TestRestorePocketPathParameterShapes(t *testing.T) {
	const invalid = `{"code":"VALIDATION","args":{"field":"id","rule":"invalid"}}`

	for _, tc := range []struct {
		name, target string
		status       int
		body         string
		restored     bool
	}{
		{"%2541 binds %41", "/api/pockets/%2541/restore", 400, invalid, false},
		{"an encoded slash", "/api/pockets/a%2fb/restore", 400, invalid, false},
		{"not a uuid", "/api/pockets/not-a-uuid/restore", 400, invalid, false},
		{"a trailing slash", "/api/pockets/" + pocketV7 + "/restore/", 404, "404 page not found", false},
		// Observed, and a surprise: the issue (#121, P10) inferred a bare 404, as
		// for /pockets/ on DELETE, but here {id} matches the empty segment, so
		// the binder refuses it ("parameter 'id' is empty") like any other
		// value that is not a uuid. Pinned as observed; reported for a ruling.
		{"an empty parameter", "/api/pockets//restore", 400, invalid, false},
		{"upper case", "/api/pockets/" + strings.ToUpper(pocketV7) + "/restore", 200, "", true},
		{"urn:uuid: prefixed", "/api/pockets/urn:uuid:" + pocketV7 + "/restore", 200, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPocketFixture(t)
			f.seed(t, pocketV7, "OVO")
			if rec := f.del(pocketV7); rec.Code != http.StatusNoContent {
				t.Fatalf("DELETE = %d", rec.Code)
			}
			before := f.row(t, pocketV7)

			rec := f.doAt(http.MethodPost, tc.target, "")
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, body(rec))
			}
			if tc.body != "" && body(rec) != tc.body {
				t.Errorf("body = %s, want %s", body(rec), tc.body)
			}
			if got := !f.deletedAt(t, pocketV7); got != tc.restored {
				t.Errorf("restored = %v, want %v", got, tc.restored)
			}
			if !tc.restored {
				if after := f.row(t, pocketV7); after != before {
					t.Errorf("the row changed:\n%s\n%s", before, after)
				}
			}
		})
	}
}
