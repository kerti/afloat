package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
)

// maxDisplayNameCodePoints is the contract's cap on a User's display_name
// (Me.display_name), so a name defaulted from a long email local part cannot
// outgrow what GET /me declares.
const maxDisplayNameCodePoints = 255

// localeFallback is what a registration naming no supported locale gets.
const localeFallback = "en-GB"

// LocalRegister validates a credential and parks it in an onboarding handshake.
// It creates no User, no Household and no session: the account is born only
// when the person commits at the gate (CommitOnboardingChoice), so a
// registration nobody finishes leaves nothing behind but a row that expires.
//
// Order, and why: the password policy runs before any hashing, so a weak
// password costs nothing; the backoff is read before the email lookup, so a
// throttled address is answered without a query it could use to enumerate; and
// the hash runs under an Argon2 permit with the backoff attempt recorded before
// that permit passes on, exactly as a failed login's is (login.go), because
// registration hashes on unauthenticated input and is the same exposure.
func (h *Handlers) LocalRegister(ctx context.Context, request api.LocalRegisterRequestObject) (api.LocalRegisterResponseObject, error) {
	body := request.Body
	email := normalizeEmail(body.Email)

	if err := ValidatePasswordPolicy(body.Password); err != nil {
		return weakPassword(err), nil
	}

	displayName, rule := registrationDisplayName(body.DisplayName, email)
	if rule != "" {
		return invalidRegistrationField("display_name", rule), nil
	}
	locale := resolveLocale(body.Locale)

	keys := registerBackoffKeys(ctx)
	budget, hasBudget := handlerBudget(ctx)

	if refused, err := h.registerThrottled(ctx, keys); err != nil || refused != nil {
		return throttledOrInternal(refused, err), nil
	}

	_, err := h.q.GetUserByEmail(ctx, email)
	switch {
	case err == nil:
		// Enumeration is accepted here, as in Balances: registering is the one
		// place that has to say an address is taken. Login never does.
		return api.LocalRegister409JSONResponse{
			ConflictJSONResponse: api.ConflictJSONResponse{Code: api.EMAILTAKEN},
		}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		logQueryError("register: look up email", err)
		resp, _ := internalError[api.LocalRegisterResponseObject]()
		return resp, nil
	}

	if err := acquireArgonPermit(ctx); err != nil {
		// As in login: ctx ended while queued, so nothing was hashed and there
		// is no attempt for the backoff to count.
		if errors.Is(err, context.Canceled) {
			slog.Warn("register: client left while waiting for an argon2 permit", "err", err)
		} else {
			slog.Error("register: wait for argon2 permit", "err", err)
		}
		resp, _ := internalError[api.LocalRegisterResponseObject]()
		return resp, nil
	}
	releasePermit := sync.OnceFunc(releaseArgonPermit)
	defer releasePermit()

	// A registration that got its permit finishes, whatever the wait cost it,
	// for the reason login's does (#33): on the request's own ctx the hash
	// could run past the deadline and the attempt then go unrecorded.
	ctx, cancel := afterPermit(ctx, budget, hasBudget)
	defer cancel()

	// Read again under the permit: a burst from one address all passed the
	// first read before any of them had been recorded.
	if refused, err := h.registerThrottled(ctx, keys); err != nil || refused != nil {
		return throttledOrInternal(refused, err), nil
	}

	hash, err := hashHoldingPermit(body.Password)
	if err != nil {
		slog.Error("register: hash password", "err", err)
		resp, _ := internalError[api.LocalRegisterResponseObject]()
		return resp, nil
	}
	h.recordFailure(ctx, keys)
	releasePermit()

	cookie, err := h.openHandshake(ctx, email, displayName, locale, hash)
	if err != nil {
		slog.Error("register: open handshake", "err", err)
		resp, _ := internalError[api.LocalRegisterResponseObject]()
		return resp, nil
	}

	setCookie := cookie.String()
	return api.LocalRegister204Response{
		Headers: api.LocalRegister204ResponseHeaders{SetCookie: &setCookie},
	}, nil
}

// registerBackoffKeys is the registration limiter's own key, under a prefix
// login does not use: sharing "ip:" would let a run of registrations lock the
// same address out of signing in, and a run of failed logins out of
// registering. Per IP only. A per-address key would let anyone throttle a
// known address's registration, and what is being limited is Argon2 work, which
// the address does not change.
func registerBackoffKeys(ctx context.Context) []string {
	if ip := clientIPFrom(ctx); ip != "" {
		return []string{"register-ip:" + ip}
	}
	return nil
}

// registerThrottled answers 429 while the backoff key is inside its window.
// A read failure comes back as err.
func (h *Handlers) registerThrottled(ctx context.Context, keys []string) (*api.LocalRegister429JSONResponse, error) {
	wait, err := h.backoffRemaining(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("read backoff: %w", err)
	}
	if wait <= 0 {
		return nil, nil
	}
	// Rounded up: a Retry-After of 0 invites an immediate retry that is still
	// inside the window.
	retryAfter := int(wait.Seconds()) + 1
	return &api.LocalRegister429JSONResponse{
		TooManyRequestsJSONResponse: api.TooManyRequestsJSONResponse{
			Body:    api.Error{Code: api.TOOMANYATTEMPTS},
			Headers: api.TooManyRequestsResponseHeaders{RetryAfter: &retryAfter},
		},
	}, nil
}

func throttledOrInternal(refused *api.LocalRegister429JSONResponse, err error) api.LocalRegisterResponseObject {
	if err != nil {
		logQueryError("register", err)
		resp, _ := internalError[api.LocalRegisterResponseObject]()
		return resp
	}
	return *refused
}

// openHandshake stores the would-be credential and returns the cookie that
// carries its token. The token is minted like a session's and only its hash is
// stored, so a database leak does not yield a presentable handshake: the row
// holds a password hash, and a leaked id would otherwise be enough to commit it.
func (h *Handlers) openHandshake(ctx context.Context, email, displayName, locale, passwordHash string) (*http.Cookie, error) {
	token, err := newSessionToken()
	if err != nil {
		return nil, err
	}

	// Swept on the way in, so the table is bounded by the registrations of one
	// TTL rather than growing with every abandoned one. Best effort: an expired
	// row is already invisible to every read, so a failed sweep costs nothing.
	if err := h.q.DeleteExpiredOnboardingHandshakes(ctx); err != nil {
		slog.Warn("register: sweep expired handshakes", "err", err)
	}

	row, err := h.q.CreateOnboardingHandshake(ctx, db.CreateOnboardingHandshakeParams{
		ID:           HashToken(token),
		Email:        email,
		DisplayName:  displayName,
		Locale:       locale,
		PasswordHash: &passwordHash,
		Ttl:          intervalFrom(onboardingHandshakeTTL),
	})
	if err != nil {
		return nil, fmt.Errorf("create handshake: %w", err)
	}
	return h.onboardingCookie(token, row.ExpiresAt.Time), nil
}

// registrationDisplayName is the trimmed name, or the email's local part when
// there is none. rule is the VALIDATION rule the name breaks, or "" when it is
// fine: `pattern` for one Postgres cannot store, `required` for one with
// nothing visible in it (blank, as the Household name's blank rule is).
func registrationDisplayName(supplied *string, email string) (name, rule string) {
	if supplied != nil {
		name = strings.TrimSpace(*supplied)
	}
	if name == "" {
		local, _, _ := strings.Cut(email, "@")
		name = truncateCodePoints(local, maxDisplayNameCodePoints)
	}
	switch {
	case !storable(name):
		return name, "pattern"
	case invisible(name):
		return name, "required"
	}
	return name, ""
}

// invisible is true for a string with nothing to see in it: only whitespace and
// format characters (Unicode category Cf: zero-width space and joiners, word
// joiner, BOM). TrimSpace leaves Cf alone, so a name of U+200B passes it and
// reads as blank on screen. It only judges: a visible name keeps its Cf
// characters, bidi controls included (Balances parity).
func invisible(s string) bool {
	return strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) }) == ""
}

// storable is false for a string Postgres refuses: a TEXT column holds no NUL,
// and the insert would otherwise fail as a 500 about a value the client chose.
func storable(s string) bool {
	return !strings.ContainsRune(s, 0)
}

func truncateCodePoints(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}

// resolveLocale is exactly one of the two supported locales, or the fallback.
// Not case-folded: the contract names two spellings, and anything else is
// "not one of them".
func resolveLocale(supplied *string) string {
	if supplied != nil {
		switch *supplied {
		case "en-GB", "id-ID":
			return *supplied
		}
	}
	return localeFallback
}

// weakPassword maps the policy's refusal onto the envelope: min and breached are
// the two reasons the contract names. ErrPasswordTooLong has no branch because
// it cannot arrive: the contract's maxLength (1024, the policy's own cap, both
// in code points) is enforced before any handler runs.
func weakPassword(err error) api.LocalRegisterResponseObject {
	reason := "min"
	if errors.Is(err, ErrPasswordCommon) {
		reason = "breached"
	}
	return api.LocalRegister400JSONResponse{
		BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: api.WEAKPASSWORD,
			Args: &map[string]interface{}{"reason": reason},
		},
	}
}

func invalidRegistrationField(field, rule string) api.LocalRegisterResponseObject {
	return api.LocalRegister400JSONResponse{
		BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: api.VALIDATION,
			Args: &map[string]interface{}{"field": field, "rule": rule},
		},
	}
}
