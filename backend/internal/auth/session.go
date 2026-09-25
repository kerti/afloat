package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/httperr"
)

// SessionCookieName is deliberately not Balances' bare `session`. Host-only
// cookies do not collide across hostnames, but a self-hoster running both apps
// behind one hostname on different paths would have them silently overwrite
// each other (BOOTSTRAP.md §5.2).
const SessionCookieName = "afloat_session"

// newSessionToken returns a fresh 256-bit URL-safe opaque value. This is the
// cookie's contents; only its hash is ever stored.
func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken is what goes in sessions.id. A database leak yields nothing usable:
// the stored value cannot be presented as a cookie.
func HashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// IssueSession mints a session and returns the cookie to set. The plaintext
// token goes to the client and only its hash is stored.
//
// It returns the cookie rather than writing it: the contract declares Set-Cookie
// on the login 204, so the generated strict response carries it and the handler
// never touches a ResponseWriter.
func (h *Handlers) IssueSession(ctx context.Context, userID pgtype.UUID, userAgent string) (*http.Cookie, error) {
	token, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	expiresAt := h.now().Add(h.sessionTTL)

	if _, err := h.q.CreateSession(ctx, db.CreateSessionParams{
		ID:        HashToken(token),
		UserID:    userID,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
		UserAgent: nullString(userAgent),
	}); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return h.sessionCookie(token, expiresAt), nil
}

func (h *Handlers) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, h.sessionCookie(token, expires))
}

func (h *Handlers) sessionCookie(token string, expires time.Time) *http.Cookie {
	// Max-Age alongside Expires, not instead of it. Spring's ResponseCookie
	// always emits both, so a cookie carrying only Expires is a header the two
	// backends do not spell the same way — and Max-Age is the one a browser
	// prefers, which makes it the one that matters on a client with a skewed
	// clock. Rounded up, so a sub-second remainder never truncates to 0 and
	// turns a fresh session into a session cookie.
	//
	// Measured against h.now(), not time.Now(): expires was computed from the
	// same seam, so time.Until here would subtract a fake clock from a real one
	// and hand a test a Max-Age with no relationship to the TTL it set.
	maxAge := max(int(math.Ceil(expires.Sub(h.now()).Seconds())), 1)
	return &http.Cookie{
		Name:  SessionCookieName,
		Value: token,
		Path:  "/",
		// No Domain attribute, deliberately: a host-only cookie cannot leak a
		// demo session into preview under a shared parent (BOOTSTRAP.md §5).
		Expires:  expires,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// ClearedSessionCookie is the cookie that expires the client's copy. Anything
// that invalidates a session owes the client this, or the browser keeps
// presenting a token that can never work.
//
// Expires is the epoch as well as Max-Age=0, because Spring's ResponseCookie
// always writes both (#32 item 2). A zero time.Time would leave Expires off,
// and the two backends' clearing headers would differ by an attribute.
func (h *Handlers) ClearedSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// ClearSessionCookie writes it directly, for the middleware paths that do hold
// a ResponseWriter.
func (h *Handlers) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, h.ClearedSessionCookie())
}

// SessionMiddleware resolves the session cookie into a User on the request
// context. It never rejects: handlers that require authentication wrap
// themselves with RequireAuth, so an unauthenticated request can still reach a
// public route.
func (h *Handlers) SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()
		hashed := HashToken(cookie.Value)

		// Both lifetimes are enforced in SQL: the sliding window, and the
		// absolute cap measured from created_at that stops a stolen cookie
		// living forever on repeated use.
		session, err := h.q.GetLiveSession(ctx, db.GetLiveSessionParams{
			ID:          hashed,
			MaxLifetime: intervalFrom(h.sessionMaxLifetime),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Expired, revoked, or never existed. Clear it so the browser
				// stops presenting a cookie that cannot work.
				h.ClearSessionCookie(w)
			} else {
				logQueryError("session lookup", err)
			}
			next.ServeHTTP(w, r)
			return
		}

		user, err := h.q.GetUserByID(ctx, session.UserID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// The session outlived its User — soft-deleted, most likely.
				h.ClearSessionCookie(w)
			} else {
				// An infrastructure failure is not a statement about this
				// user's session: leave the cookie alone. The request
				// proceeds unauthenticated, same as the session-lookup branch
				// above (#27).
				logQueryError("session user lookup", err)
			}
			next.ServeHTTP(w, r)
			return
		}

		h.touchIfStale(ctx, w, session, cookie.Value)
		next.ServeHTTP(w, r.WithContext(WithUser(ctx, user)))
	})
}

// touchIfStale extends the sliding window, but only once the session is past
// half its life.
//
// Balances writes on every authenticated request, which makes every GET a write
// on the one table read by every request. The observable behaviour is the same
// and the write volume is a fraction of it (BOOTSTRAP.md §5.1).
func (h *Handlers) touchIfStale(ctx context.Context, w http.ResponseWriter, session db.Session, token string) {
	remaining := time.Until(session.ExpiresAt.Time)
	if remaining > h.sessionTTL/2 {
		return
	}

	newExpiry := h.now().Add(h.sessionTTL)
	if err := h.q.TouchSession(ctx, db.TouchSessionParams{
		ID:        session.ID,
		ExpiresAt: pgtype.Timestamptz{Time: newExpiry, Valid: true},
	}); err != nil {
		// A failed refresh is not worth failing the request: the session is
		// still valid until its current expiry.
		slog.Warn("touch session", "err", err)
		return
	}
	// The cookie must keep the PLAINTEXT the client presented. Writing the
	// hashed id here would mean the next request's hash never matches this row.
	h.setSessionCookie(w, token, newExpiry)
}

// RequireAuth rejects a request that SessionMiddleware did not attach a User to.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFromContext(r.Context()); !ok {
			httperr.Write(w, http.StatusUnauthorized, httperr.CodeUnauthorized, nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// intervalFrom converts a duration to a Postgres interval. Microseconds only —
// months and days are calendar-relative and would make the absolute session cap
// depend on which month it is.
func intervalFrom(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}
