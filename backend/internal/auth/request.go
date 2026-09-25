package auth

import (
	"context"
	"net"
	"net/http"
	"strings"
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
// presented session token into the handler context.
func RequestContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), clientIPKey{}, clientIP(r))
		ctx = context.WithValue(ctx, userAgentKey{}, r.UserAgent())
		if cookie, err := r.Cookie(SessionCookieName); err == nil {
			ctx = context.WithValue(ctx, sessionTokenKey{}, cookie.Value)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
