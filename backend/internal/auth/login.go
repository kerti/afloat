package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
)

// dummyHash is verified against when no credential exists, so a request for an
// unknown address costs the same Argon2id work as a real one. Without it,
// response time answers "does this account exist?" regardless of what the
// status code says. Generated once at startup from a value nobody knows.
var dummyHash = func() string {
	// Package init: no request to carry a deadline, nothing yet holding a permit.
	h, err := HashPassword(context.Background(), "this password matches nothing, by construction")
	if err != nil {
		// Only reachable if crypto/rand fails, in which case nothing about this
		// process is trustworthy.
		panic("auth: cannot build dummy hash: " + err.Error())
	}
	return h
}()

// LocalLogin exchanges email and password for a session.
//
// Every failure mode — unknown email, a User holding no credential, a wrong
// password — returns one INVALID_CREDENTIALS, the comparison is constant time,
// and a request for an address with no account still pays the full hashing
// cost. All three, not two of three: any one missing re-opens enumeration.
func (h *Handlers) LocalLogin(ctx context.Context, request api.LocalLoginRequestObject) (api.LocalLoginResponseObject, error) {
	email := normalizeEmail(string(request.Body.Email))
	password := request.Body.Password

	keys := backoffKeys(ctx, email)

	// Read once before queueing, so a throttled caller is answered without
	// waiting for a permit. checkPassword reads it again under the permit.
	if refused := h.throttled(ctx, keys); refused != nil {
		return refused, nil
	}

	user, phc := h.resolve(ctx, email)

	refused, ok, err := h.checkPassword(ctx, keys, password, phc)
	if err != nil {
		// ctx ended while queued for an Argon2 permit (#33): the handler
		// timeout passed, or the client went away. The password was never
		// checked, so this is neither INVALID_CREDENTIALS nor a failure for the
		// backoff to count. A client leaving is no fault of the server's, and
		// a stream of dropped connections must not become a stream of errors.
		if errors.Is(err, context.Canceled) {
			slog.Warn("login: client left while waiting for an argon2 permit", "err", err)
		} else {
			slog.Error("login: wait for argon2 permit", "err", err)
		}
		return internalError[api.LocalLoginResponseObject]()
	}
	if refused != nil {
		return refused, nil
	}
	if !ok {
		return invalidCredentials()
	}

	if err := h.q.ClearLoginAttempts(ctx, keys); err != nil {
		// The login succeeded; a stale backoff row is a nuisance, not a reason
		// to refuse the session.
		slog.Warn("login: clear attempts", "err", err)
	}

	cookie, err := h.IssueSession(ctx, user.ID, userAgentFrom(ctx))
	if err != nil {
		slog.Error("login: issue session", "err", err)
		return internalError[api.LocalLoginResponseObject]()
	}

	// The contract declares Set-Cookie on this 204, so the generated response
	// carries it and the handler never touches a ResponseWriter.
	setCookie := cookie.String()
	return api.LocalLogin204Response{
		Headers: api.LocalLogin204ResponseHeaders{SetCookie: &setCookie},
	}, nil
}

// throttled answers 429 while a backoff key is inside its window, or 500 if
// the backoff cannot be read; nil lets the login go ahead.
func (h *Handlers) throttled(ctx context.Context, keys []string) api.LocalLoginResponseObject {
	wait, err := h.backoffRemaining(ctx, keys)
	if err != nil {
		slog.Error("login: read backoff", "err", err)
		resp, _ := internalError[api.LocalLoginResponseObject]()
		return resp
	}
	if wait <= 0 {
		return nil
	}
	// Rounded up: a Retry-After of 0 invites an immediate retry that is still
	// inside the window.
	retryAfter := int(wait.Seconds()) + 1
	return api.LocalLogin429JSONResponse{
		TooManyRequestsJSONResponse: api.TooManyRequestsJSONResponse{
			Body:    api.Error{Code: api.TOOMANYATTEMPTS},
			Headers: api.TooManyRequestsResponseHeaders{RetryAfter: &retryAfter},
		},
	}
}

// resolve finds the hash to check the password against, and the User it
// belongs to. With no credential it answers the dummy hash and no User, so an
// unknown or dormant address costs the same work as a real one.
func (h *Handlers) resolve(ctx context.Context, email string) (db.User, string) {
	user, err := h.q.GetUserByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("login: look up user", "err", err)
		}
		return db.User{}, dummyHash
	}

	cred, err := h.q.GetCredentialByUserID(ctx, user.ID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("login: look up credential", "err", err)
		}
		// A dormant User — invited, never set a password. Same cost, same answer.
		return db.User{}, dummyHash
	}
	return user, cred.PasswordHash
}

// checkPassword holds one Argon2 permit from a second backoff read until any
// failure is recorded. The first read, before the queue, sees none of the
// failures of the logins queued alongside it: a burst on one account all
// passed it, all queued, and all had their passwords checked, the backoff
// never applying (#33). Read under the permit instead, and written before the
// permit passes on, one caller's failure throttles the next. At most the cap's
// worth of callers are checked at once, so a burst gets that many guesses, not
// the whole queue. An error means no permit came free before ctx ended.
func (h *Handlers) checkPassword(ctx context.Context, keys []string, password, phc string) (refused api.LocalLoginResponseObject, ok bool, err error) {
	if err := acquireArgonPermit(ctx); err != nil {
		return nil, false, err
	}
	defer releaseArgonPermit()

	if refused := h.throttled(ctx, keys); refused != nil {
		return refused, false, nil
	}
	if !verifyHoldingPermit(password, phc) {
		h.recordFailure(ctx, keys)
		return nil, false, nil
	}
	return nil, true, nil
}

// backoffRemaining reads the remaining backoff as seconds the database itself
// computed (query: GetLoginBackoff), never subtracting against h.now(). The
// window's start (backoff_until) and its end (the database's now()) must come
// from the same clock, or app/DB skew makes Retry-After lie and, if the app
// clock leads, can let the throttle silently stop applying near the end of a
// window (#25).
func (h *Handlers) backoffRemaining(ctx context.Context, keys []string) (time.Duration, error) {
	remainingSeconds, err := h.q.GetLoginBackoff(ctx, keys)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	if remainingSeconds <= 0 {
		return 0, nil
	}
	return time.Duration(remainingSeconds * float64(time.Second)), nil
}

// recordFailure writes every key's row independently: email: and ip: are two
// separate rate limiters, so one failing to write (a transient DB error) must
// not stop the other from being recorded — that is exactly when the per-IP
// limiter matters most. Logs and continues rather than returning on the first
// error (#32 item 3).
func (h *Handlers) recordFailure(ctx context.Context, keys []string) {
	for _, key := range keys {
		if err := h.q.RecordLoginFailure(ctx, db.RecordLoginFailureParams{
			Key:          key,
			FirstBackoff: intervalFrom(firstBackoff),
			MaxBackoff:   intervalFrom(maxBackoff),
		}); err != nil {
			slog.Error("login: record failure", "key", key, "err", err)
		}
	}
}

// backoffKeys rate-limits per IP and per email together: per-IP alone lets one
// attacker spread across addresses, per-email alone lets anyone lock out a
// known address. The pair is checked in a single read.
func backoffKeys(ctx context.Context, email string) []string {
	keys := []string{"email:" + email}
	if ip := clientIPFrom(ctx); ip != "" {
		keys = append(keys, "ip:"+ip)
	}
	return keys
}

// normalizeEmail lower-cases and trims so the lookup matches the same
// expression the unique index uses.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func invalidCredentials() (api.LocalLoginResponseObject, error) {
	return api.LocalLogin401JSONResponse{
		UnauthorizedJSONResponse: api.UnauthorizedJSONResponse{Code: api.INVALIDCREDENTIALS},
	}, nil
}
