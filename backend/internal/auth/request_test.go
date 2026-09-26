package auth

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// R1: the storage rule for sessions.user_agent, in order - absent/empty is
// NULL (#32 item 4, unchanged), not-valid-UTF-8 is NULL (a stored invalid
// header used to fail the whole login with a Postgres 500), otherwise
// truncated to 512 Unicode code points on a code point boundary. Kotlin's
// RequestFactsFilterSpec holds the same rows.
func TestSanitizeUserAgent(t *testing.T) {
	longEmoji := strings.Repeat("a", 511) + "💚" + "a" // 513 code points, multibyte at the cut

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"empty is empty (nullString turns this into NULL)", "", ""},
		{"ordinary ASCII passes through", "conformance/1.0", "conformance/1.0"},
		{"valid multibyte UTF-8 passes through", "Afloat — test 🔐", "Afloat — test 🔐"},
		{"lone continuation byte 0x85 is invalid UTF-8", "x\x85y", ""},
		{"overlong encoding C0 AF is invalid UTF-8", "x\xc0\xafy", ""},
		{"a truncated 3-byte sequence is invalid UTF-8", "x\xe2\x80y", ""},
		{"a lone 0xFF is invalid UTF-8", "x\xffy", ""},
		{"Latin-1 'café' (not UTF-8) is invalid UTF-8", "caf\xe9", ""},
		{"valid text with one invalid byte is invalid UTF-8 as a whole", "abc\x85def", ""},
		{"511 code points is untouched", strings.Repeat("a", 511), strings.Repeat("a", 511)},
		{
			"512 code points, multibyte at the boundary, is untouched",
			strings.Repeat("a", 511) + "💚",
			strings.Repeat("a", 511) + "💚",
		},
		{
			"513 code points, multibyte astride the cut, truncates to 512 without splitting it",
			longEmoji,
			strings.Repeat("a", 511) + "💚",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeUserAgent(tc.raw)
			if got != tc.want {
				t.Errorf("sanitizeUserAgent(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("sanitizeUserAgent(%q) = %q, which is not valid UTF-8", tc.raw, got)
			}
			if n := utf8.RuneCountInString(got); n > maxUserAgentCodePoints {
				t.Errorf("sanitizeUserAgent(%q) has %d code points, want at most %d", tc.raw, n, maxUserAgentCodePoints)
			}
		})
	}
}
