package conformance_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kerti/afloat/conformance"
)

// The cookie comparison is where the harness stops comparing bytes, so these
// prove it still catches every difference it claims to, and forgives only the
// two it names: a minted value, and Expires measured against Date.

var date = time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)

func parseOne(t *testing.T, line string) conformance.SetCookie {
	t.Helper()
	cs, err := conformance.ParseSetCookies(http.Header{"Set-Cookie": {line}})
	if err != nil || len(cs) != 1 {
		t.Fatalf("parse %q: %v, %d cookie(s)", line, err, len(cs))
	}
	return cs[0]
}

func sessionExpect() conformance.CookieExpect {
	return conformance.CookieExpect{
		ValuePattern: `[A-Za-z0-9_-]{4}`,
		Path:         new("/"),
		MaxAge:       new(3600),
		HttpOnly:     new(true),
		Secure:       new(false),
		SameSite:     "Lax",
		Expires:      conformance.ExpiresMaxAge,
	}
}

const goodSession = "afloat_session=ab-_; Path=/; Expires=Fri, 25 Sep 2026 09:00:00 GMT; Max-Age=3600; HttpOnly; SameSite=Lax"

func TestCheckCookieAcceptsAMatch(t *testing.T) {
	if bad := conformance.CheckCookie(parseOne(t, goodSession), sessionExpect(), date); len(bad) > 0 {
		t.Fatalf("want no mismatch, got %v", bad)
	}
}

func TestCheckCookieCatchesEachAttribute(t *testing.T) {
	for _, tc := range []struct {
		name, line, want string
	}{
		{"value", "afloat_session=a; Path=/; Expires=Fri, 25 Sep 2026 09:00:00 GMT; Max-Age=3600; HttpOnly; SameSite=Lax", "does not match"},
		{"path", "afloat_session=abcd; Path=/api; Expires=Fri, 25 Sep 2026 09:00:00 GMT; Max-Age=3600; HttpOnly; SameSite=Lax", "Path"},
		{"domain", goodSession + "; Domain=example.com", "host-only"},
		{"max-age", "afloat_session=abcd; Path=/; Expires=Fri, 25 Sep 2026 09:00:00 GMT; Max-Age=60; HttpOnly; SameSite=Lax", "Max-Age"},
		{"max-age absent", "afloat_session=abcd; Path=/; Expires=Fri, 25 Sep 2026 09:00:00 GMT; HttpOnly; SameSite=Lax", "Max-Age"},
		{"httponly", "afloat_session=abcd; Path=/; Expires=Fri, 25 Sep 2026 09:00:00 GMT; Max-Age=3600; SameSite=Lax", "HttpOnly"},
		{"secure", goodSession + "; Secure", "Secure"},
		{"samesite", "afloat_session=abcd; Path=/; Expires=Fri, 25 Sep 2026 09:00:00 GMT; Max-Age=3600; HttpOnly; SameSite=Strict", "SameSite"},
		{"expires a day off", "afloat_session=abcd; Path=/; Expires=Sat, 26 Sep 2026 09:00:00 GMT; Max-Age=3600; HttpOnly; SameSite=Lax", "not Date + Max-Age"},
		{"expires absent", "afloat_session=abcd; Path=/; Max-Age=3600; HttpOnly; SameSite=Lax", "not Date + Max-Age"},
		{"an attribute no case asserts", goodSession + "; Partitioned", "no case asserts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := conformance.CheckCookie(parseOne(t, tc.line), sessionExpect(), date)
			if !strings.Contains(strings.Join(bad, "\n"), tc.want) {
				t.Fatalf("want a mismatch mentioning %q, got %v", tc.want, bad)
			}
		})
	}
}

// Spring writes an unpadded day of the month; RFC 1123 allows it.
func TestCheckCookieReadsBothSpellingsOfTheEpoch(t *testing.T) {
	want := conformance.CookieExpect{
		Value: new(""), Path: new("/"), MaxAge: new(0), HttpOnly: new(true), Secure: new(false),
		SameSite: "Lax", Expires: conformance.ExpiresPast,
	}
	for _, e := range []string{"Thu, 01 Jan 1970 00:00:00 GMT", "Thu, 1 Jan 1970 00:00:00 GMT"} {
		line := "afloat_session=; Path=/; Max-Age=0; Expires=" + e + "; HttpOnly; SameSite=Lax"
		if bad := conformance.CheckCookie(parseOne(t, line), want, date); len(bad) > 0 {
			t.Errorf("%s: want no mismatch, got %v", e, bad)
		}
	}
	absent := parseOne(t, "afloat_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax")
	if bad := conformance.CheckCookie(absent, want, date); !strings.Contains(strings.Join(bad, ""), "is absent, want past") {
		t.Errorf("an absent Expires must fail `past`, got %v", bad)
	}
}

func TestCookieParityForgivesOnlyTheMintedParts(t *testing.T) {
	a := parseOne(t, "afloat_session=abcd; Path=/; Expires=Fri, 25 Sep 2026 09:00:00 GMT; Max-Age=3600; HttpOnly; SameSite=Lax")
	// Another token, another second, attributes in another order and case.
	b := parseOne(t, "afloat_session=wxyz; Max-Age=3600; path=/; expires=Fri, 25 Sep 2026 09:00:05 GMT; SameSite=Lax; HttpOnly")
	if bad := conformance.CookieParity([]conformance.SetCookie{a}, []conformance.SetCookie{b}, date, date.Add(5*time.Second)); len(bad) > 0 {
		t.Fatalf("want parity, got %v", bad)
	}
}

func TestCookieParityCatchesARealDifference(t *testing.T) {
	base := "afloat_session=abcd; Path=/; Expires=Fri, 25 Sep 2026 09:00:00 GMT; Max-Age=3600; HttpOnly; SameSite=Lax"
	for _, tc := range []struct {
		name, other, want string
	}{
		{"an attribute only one sends", base + "; Secure", "attribute secure"},
		{"an attribute value", strings.Replace(base, "Max-Age=3600", "Max-Age=3599", 1), "attribute max-age"},
		{"value case in SameSite", strings.Replace(base, "Lax", "lax", 1), "attribute samesite"},
		{"token length", strings.Replace(base, "abcd", "abcde", 1), "value differs in shape"},
		{"Expires sent by one only", strings.Replace(base, "Expires=Fri, 25 Sep 2026 09:00:00 GMT; ", "", 1), "attribute expires"},
		{"Expires past on one, future on the other", strings.Replace(base, "Fri, 25 Sep 2026 09:00:00", "Thu, 01 Jan 1970 00:00:00", 1), "Expires"},
		{"Expires a minute apart", strings.Replace(base, "09:00:00", "09:01:00", 1), "Expires"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := conformance.CookieParity(
				[]conformance.SetCookie{parseOne(t, base)}, []conformance.SetCookie{parseOne(t, tc.other)}, date, date)
			if !strings.Contains(strings.Join(bad, "\n"), tc.want) {
				t.Fatalf("want a difference mentioning %q, got %v", tc.want, bad)
			}
		})
	}
	if bad := conformance.CookieParity(nil, []conformance.SetCookie{parseOne(t, base)}, date, date); len(bad) == 0 {
		t.Error("a cookie set by one backend only must be a difference")
	}
}

// The clearing cookie Go and Spring each emit (#32 item 2) must now be one
// cookie as far as parity is concerned, though the two spell Expires apart.
func TestCookieParityOnTheClearingCookie(t *testing.T) {
	g := parseOne(t, "afloat_session=; Path=/; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Max-Age=0; HttpOnly; SameSite=Lax")
	k := parseOne(t, "afloat_session=; Path=/; Max-Age=0; Expires=Thu, 1 Jan 1970 00:00:00 GMT; HttpOnly; SameSite=Lax")
	if bad := conformance.CookieParity([]conformance.SetCookie{g}, []conformance.SetCookie{k}, date, date.Add(3*time.Second)); len(bad) > 0 {
		t.Fatalf("want parity, got %v", bad)
	}
}

func TestSnapshotParity(t *testing.T) {
	text := func(s string) conformance.SnapshotCell { return conformance.SnapshotCell{Text: &s} }
	at := func(f float64) conformance.SnapshotCell { return conformance.SnapshotCell{Offset: &f} }
	null := conformance.SnapshotCell{}
	row := func(ua conformance.SnapshotCell, created float64) conformance.SnapshotRow {
		return conformance.SnapshotRow{"user_agent": ua, "created_at": at(created)}
	}
	snap := func(rows ...conformance.SnapshotRow) conformance.Snapshot {
		return conformance.Snapshot{"sessions": rows}
	}

	if bad := conformance.SnapshotParity(snap(row(null, -0.1)), snap(row(null, -1.5))); len(bad) > 0 {
		t.Errorf("rows a second and a half apart in age must match, got %v", bad)
	}
	for _, tc := range []struct {
		name string
		a, b conformance.Snapshot
	}{
		// #32 item 4 is exactly this shape.
		{"NULL against empty text", snap(row(null, 0)), snap(row(text(""), 0))},
		{"two texts", snap(row(text("a"), 0)), snap(row(text("b"), 0))},
		{"a timestamp an hour off", snap(row(null, 0)), snap(row(null, 3600))},
		{"a NULL timestamp", snap(row(null, 0)), snap(conformance.SnapshotRow{"user_agent": null, "created_at": null})},
		{"a missing row", snap(row(null, 0), row(text("a"), 0)), snap(row(null, 0))},
		{"a table only one has rows in", conformance.Snapshot{}, snap(row(null, 0))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if bad := conformance.SnapshotParity(tc.a, tc.b); len(bad) == 0 {
				t.Fatal("want a difference, got none")
			}
		})
	}
}
