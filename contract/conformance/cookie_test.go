package conformance_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kerti/afloat/conformance"
)

// The cookie assertion is where the harness stops comparing bytes, so these
// prove it still catches every attribute it claims to, and measures Expires
// against Date rather than as an instant.

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

// An unpadded day of the month is RFC 1123-valid, so it must parse too.
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
