package conformance

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ExpiresTolerance is how far an Expires may sit from Date plus Max-Age. Both
// are whole seconds, and a backend may read its clock for the cookie a moment
// before or after the one that stamps Date.
const ExpiresTolerance = 2 * time.Second

// SetCookie is one Set-Cookie header, split into its parts and nothing more.
// Hand-parsed rather than through http.ParseSetCookie, which folds Max-Age=0
// into -1, drops attributes it does not know, and keeps no record of whether
// Expires was sent - exactly the details a parity check needs to see.
type SetCookie struct {
	Name  string
	Value string
	// Attrs is keyed by lower-cased attribute name; a flag such as HttpOnly
	// maps to "". A repeated attribute keeps its last value, as browsers do.
	Attrs map[string]string
	Raw   string
}

func ParseSetCookies(h http.Header) ([]SetCookie, error) {
	var out []SetCookie
	for _, line := range h.Values("Set-Cookie") {
		parts := strings.Split(line, ";")
		name, value, ok := strings.Cut(strings.TrimSpace(parts[0]), "=")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("unparseable Set-Cookie %q", line)
		}
		c := SetCookie{Name: strings.TrimSpace(name), Value: strings.TrimSpace(value), Attrs: map[string]string{}, Raw: line}
		for _, a := range parts[1:] {
			k, v, _ := strings.Cut(strings.TrimSpace(a), "=")
			if k == "" {
				continue
			}
			c.Attrs[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
		out = append(out, c)
	}
	return out, nil
}

// expiresRelation is the Expires attribute reduced to what a case can assert:
// absent, at or before Date, or its distance past Date.
func expiresRelation(c SetCookie, date time.Time) (class string, ahead time.Duration, err error) {
	raw, ok := c.Attrs["expires"]
	if !ok {
		return ExpiresAbsent, 0, nil
	}
	at, err := http.ParseTime(raw)
	if err != nil {
		// Spring writes the day of the month unpadded ("Thu, 1 Jan 1970"),
		// which RFC 1123 allows and http.TimeFormat does not.
		at, err = time.Parse("Mon, 2 Jan 2006 15:04:05 GMT", raw)
	}
	if err != nil {
		return "", 0, fmt.Errorf("unparseable Expires %q", raw)
	}
	if date.IsZero() {
		return "", 0, fmt.Errorf("Expires %q but no Date to measure it against", raw)
	}
	if !at.After(date) {
		return ExpiresPast, 0, nil
	}
	return "future", at.Sub(date), nil
}

// CheckCookie holds one Set-Cookie to its expectation and returns every
// mismatch, not just the first, so one run shows the whole attribute set.
func CheckCookie(c SetCookie, want CookieExpect, date time.Time) []string {
	var bad []string
	if want.Value != nil && c.Value != *want.Value {
		bad = append(bad, fmt.Sprintf("value %q, want %q", c.Value, *want.Value))
	}
	if want.ValuePattern != "" && !regexp.MustCompile(`\A(?:`+want.ValuePattern+`)\z`).MatchString(c.Value) {
		bad = append(bad, fmt.Sprintf("value %q does not match %s", c.Value, want.ValuePattern))
	}
	if got := c.Attrs["path"]; got != *want.Path {
		bad = append(bad, fmt.Sprintf("Path %q, want %q", got, *want.Path))
	}
	if got, ok := c.Attrs["domain"]; want.Domain == "" && ok {
		bad = append(bad, fmt.Sprintf("Domain %q, want none (host-only)", got))
	} else if want.Domain != "" && got != want.Domain {
		bad = append(bad, fmt.Sprintf("Domain %q, want %q", got, want.Domain))
	}
	maxAge, hasMaxAge := c.Attrs["max-age"]
	if !hasMaxAge || maxAge != strconv.Itoa(*want.MaxAge) {
		bad = append(bad, fmt.Sprintf("Max-Age %q (sent: %v), want %d", maxAge, hasMaxAge, *want.MaxAge))
	}
	if _, ok := c.Attrs["httponly"]; ok != *want.HttpOnly {
		bad = append(bad, fmt.Sprintf("HttpOnly %v, want %v", ok, *want.HttpOnly))
	}
	if _, ok := c.Attrs["secure"]; ok != *want.Secure {
		bad = append(bad, fmt.Sprintf("Secure %v, want %v", ok, *want.Secure))
	}
	if got := c.Attrs["samesite"]; got != want.SameSite {
		bad = append(bad, fmt.Sprintf("SameSite %q, want %q", got, want.SameSite))
	}

	class, ahead, err := expiresRelation(c, date)
	switch {
	case err != nil:
		bad = append(bad, err.Error())
	case want.Expires == ExpiresMaxAge:
		d := ahead - time.Duration(*want.MaxAge)*time.Second
		if class != "future" || d < -ExpiresTolerance || d > ExpiresTolerance {
			bad = append(bad, fmt.Sprintf("Expires %q is not Date + Max-Age (Date %s)", c.Attrs["expires"], date.Format(http.TimeFormat)))
		}
	case class != want.Expires:
		bad = append(bad, fmt.Sprintf("Expires %q is %s, want %s", c.Attrs["expires"], class, want.Expires))
	}

	known := map[string]bool{"path": true, "domain": true, "max-age": true, "httponly": true, "secure": true, "samesite": true, "expires": true}
	for k := range c.Attrs {
		if !known[k] {
			bad = append(bad, fmt.Sprintf("attribute %q is sent and no case asserts it", k))
		}
	}
	sort.Strings(bad)
	return bad
}

// CookieParity compares two backends' Set-Cookie headers attribute by
// attribute rather than as bytes.
//
// Two parts of a Set-Cookie differ by construction and are compared by what
// they mean instead: a minted value (its length, and whether it is empty),
// and Expires (absent, past, or how far past Date - so Spring's unpadded day
// of the month and a second's skew between two clocks both pass, while a
// cookie that expires a day early does not). Everything else must match
// exactly, so attribute order and the attribute name's case are the only
// freedoms.
func CookieParity(a, b []SetCookie, aDate, bDate time.Time) []string {
	var bad []string
	byName := func(cs []SetCookie) map[string][]SetCookie {
		m := map[string][]SetCookie{}
		for _, c := range cs {
			m[c.Name] = append(m[c.Name], c)
		}
		return m
	}
	am, bm := byName(a), byName(b)
	names := map[string]bool{}
	for n := range am {
		names[n] = true
	}
	for n := range bm {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	for _, n := range sorted {
		as, bs := am[n], bm[n]
		if len(as) != len(bs) {
			bad = append(bad, fmt.Sprintf("cookie %s is set %d time(s) by go, %d by kotlin", n, len(as), len(bs)))
			continue
		}
		for i := range as {
			ac, bc := as[i], bs[i]
			if (ac.Value == "") != (bc.Value == "") || len(ac.Value) != len(bc.Value) {
				bad = append(bad, fmt.Sprintf("cookie %s value differs in shape: go %d byte(s), kotlin %d", n, len(ac.Value), len(bc.Value)))
			}
			keys := map[string]bool{}
			for k := range ac.Attrs {
				keys[k] = true
			}
			for k := range bc.Attrs {
				keys[k] = true
			}
			for k := range keys {
				av, aok := ac.Attrs[k]
				bv, bok := bc.Attrs[k]
				switch {
				case aok != bok:
					bad = append(bad, fmt.Sprintf("cookie %s attribute %s: sent by go %v, by kotlin %v", n, k, aok, bok))
				case k == "expires":
					aClass, aAhead, aErr := expiresRelation(ac, aDate)
					bClass, bAhead, bErr := expiresRelation(bc, bDate)
					d := aAhead - bAhead
					switch {
					case aErr != nil || bErr != nil:
						bad = append(bad, fmt.Sprintf("cookie %s Expires: go %v, kotlin %v", n, aErr, bErr))
					case aClass != bClass || d < -ExpiresTolerance || d > ExpiresTolerance:
						bad = append(bad, fmt.Sprintf("cookie %s Expires: go %q (%s), kotlin %q (%s)", n, av, aClass, bv, bClass))
					}
				case av != bv:
					bad = append(bad, fmt.Sprintf("cookie %s attribute %s: go %q, kotlin %q", n, k, av, bv))
				}
			}
		}
	}
	sort.Strings(bad)
	return bad
}
