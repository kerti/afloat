package auth

import (
	"context"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"
)

// Strict handlers receive only a context, not the request. The few
// request-scoped facts authentication needs are put there by
// RequestContextMiddleware rather than reaching for globals.
type (
	clientIPKey     struct{}
	userAgentKey    struct{}
	sessionTokenKey struct{}
)

// RequestContextMiddleware carries the client IP, the User-Agent and the
// presented session token into the handler context. The User-Agent is
// sanitised for storage here (sanitizeUserAgent), not left as the raw header:
// an invalid one reaching Postgres unsanitised failed the whole login with a
// 500 ("invalid byte sequence for encoding \"UTF8\": 0x85"), and userAgentFrom
// has exactly one caller (login.go), so there is no other use this would
// short-change.
func RequestContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), clientIPKey{}, clientIP(r))
		ctx = context.WithValue(ctx, userAgentKey{}, sanitizeUserAgent(r.UserAgent()))
		if cookie, err := r.Cookie(SessionCookieName); err == nil {
			ctx = context.WithValue(ctx, sessionTokenKey{}, cookie.Value)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// maxUserAgentCodePoints bounds what sessions.user_agent stores. Unicode code
// points, like every other length limit in this codebase (BOOTSTRAP.md §5.1) -
// never bytes, never UTF-16 units - so a User-Agent that happens to carry
// multibyte characters is not penalised for encoding wider, and so Go and
// Kotlin cut at the same character regardless of how many bytes or UTF-16
// units it took to get there.
const maxUserAgentCodePoints = 512

// sanitizeUserAgent applies the storage rule for sessions.user_agent, in
// order (#70, on top of #32 item 4's original empty-is-NULL rule):
//
//  1. Fold HTAB to SP, then trim SP/HTAB from both ends. The two connectors
//     unfold an obsolete header fold (RFC 7230 §3.2.4's obs-fold) differently
//     - Go collapses "CRLF 1*(SP/HTAB)" to a single SP; Tomcat drops only the
//     CRLF and keeps the fold's own whitespace verbatim, tab included - so
//     "abc\r\n\tdef" reaches this function as "abc def" from Go but
//     "abc\tdef" from Kotlin. Folding HTAB to SP and trimming both ends
//     converges every case the review measured (an internal tab, a leading fold with
//     nothing before it, a trailing fold with nothing after) to the same
//     stored string on both backends, without either connector's own
//     unfolding needing to agree in the first place. A real inline tab a
//     client meant literally is stored as a space; nothing here can tell it
//     apart from a fold's byte, because past this rewrite there is no
//     difference to tell apart.
//  2. Absent or empty: NULL. nullString's existing "" -> nil already does
//     this, so this returns "" unchanged.
//  3. Not valid UTF-8: NULL. Go's r.UserAgent() carries a header's raw bytes
//     through untouched (net/http never validates them), and a Postgres TEXT
//     column is UTF8-encoded - so a client that sent invalid UTF-8 used to
//     fail CreateSession, and the whole login, with a 500 that had nothing to
//     do with the password. A malformed header is not a database outage
//     either, so this is refused quietly, the same way a malformed Argon2
//     hash is (auth/password.go), not surfaced as an error.
//  4. Otherwise, truncated to maxUserAgentCodePoints Unicode code points, on
//     a code point boundary. Runes ARE Go's Unicode code points, so slicing a
//     []rune conversion can only ever cut between two of them, never through
//     one - safe here specifically because step 3 already guarantees valid
//     UTF-8, which is what makes a []rune round-trip lossless.
func sanitizeUserAgent(raw string) string {
	raw = strings.Trim(strings.ReplaceAll(raw, "\t", " "), " ")
	if raw == "" {
		return ""
	}
	if !utf8.ValidString(raw) {
		return ""
	}
	if utf8.RuneCountInString(raw) <= maxUserAgentCodePoints {
		return raw
	}
	runes := []rune(raw)
	return string(runes[:maxUserAgentCodePoints])
}

func clientIPFrom(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}

func userAgentFrom(ctx context.Context) string {
	ua, _ := ctx.Value(userAgentKey{}).(string)
	return ua
}

func sessionTokenFrom(ctx context.Context) string {
	token, _ := ctx.Value(sessionTokenKey{}).(string)
	return token
}

// clientIP is the address the connection came from.
//
// It deliberately does NOT read X-Forwarded-For. Afloat is self-hosted with no
// guaranteed proxy, so that header is attacker-controlled — and using it for a
// rate-limit key means an attacker picks a fresh key per request and the
// per-IP backoff stops existing. chi's RealIP middleware is likewise not
// mounted in front of this. If a deployment ever puts a trusted proxy in front,
// that is a config-gated change, not a default.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}
