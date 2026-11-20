package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
)

// OnboardingCookieName carries the opaque handshake token through the gate. Its
// own name, not the session's: holding a handshake is not holding a session.
const OnboardingCookieName = "afloat_onboarding"

// onboardingHandshakeTTL is how long a registered but uncommitted credential
// may sit at the gate before the person must register again. Fixed by ruling,
// not an operator knob.
const onboardingHandshakeTTL = 15 * time.Minute

// maxHouseholdNameCodePoints caps the Household name, in code points like every
// other length in this codebase (BOOTSTRAP.md §5.1).
const maxHouseholdNameCodePoints = 60

const (
	pgUniqueViolation = "23505"
	usersEmailIndex   = "users_email_idx"
)

func (h *Handlers) onboardingCookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:  OnboardingCookieName,
		Value: token,
		Path:  "/",
		// Host-only, like the session cookie (BOOTSTRAP.md §5): no Domain.
		Expires:  expires,
		MaxAge:   int(math.Ceil(onboardingHandshakeTTL.Seconds())),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// ClearedOnboardingCookie expires the client's copy, as ClearedSessionCookie
// does, Expires as well as Max-Age=0 so a client that ignores one still drops
// it.
func (h *Handlers) ClearedOnboardingCookie() *http.Cookie {
	return &http.Cookie{
		Name:     OnboardingCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// handshakeLookup is what the presented handshake cookie resolved to.
type handshakeLookup struct {
	row db.OnboardingHandshake
	// live is false for no cookie, an unknown token and an expired one alike:
	// the three are one answer, as they are for a session.
	live bool
	// presented is true when a cookie was sent, so a dead one is cleared and an
	// absent one is not given a Set-Cookie it never needed.
	presented bool
}

// lookupHandshake resolves the presented cookie. Only pgx.ErrNoRows means the
// handshake is dead; any other failure is an outage and is returned, so the
// caller answers 500 and leaves the cookie alone (#27's rule for sessions).
func (h *Handlers) lookupHandshake(ctx context.Context) (handshakeLookup, error) {
	token := onboardingTokenFrom(ctx)
	if token == "" {
		return handshakeLookup{}, nil
	}
	row, err := h.q.GetLiveOnboardingHandshake(ctx, HashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return handshakeLookup{presented: true}, nil
	}
	if err != nil {
		return handshakeLookup{}, fmt.Errorf("look up handshake: %w", err)
	}
	return handshakeLookup{row: row, live: true, presented: true}, nil
}

// onboardingUnauthorized is the 401 for a dead handshake. It clears a stale
// cookie and says nothing else: no expiry, no reason.
func (h *Handlers) onboardingUnauthorized(presented bool) api.OnboardingUnauthorizedJSONResponse {
	resp := api.OnboardingUnauthorizedJSONResponse{Body: api.Error{Code: api.UNAUTHORIZED}}
	if presented {
		cleared := h.ClearedOnboardingCookie().String()
		resp.Headers.SetCookie = &cleared
	}
	return resp
}

// GetOnboardingOptions is what the gate offers the holder of a handshake. The
// handshake cookie is the credential; no session is involved.
func (h *Handlers) GetOnboardingOptions(ctx context.Context, _ api.GetOnboardingOptionsRequestObject) (api.GetOnboardingOptionsResponseObject, error) {
	hs, err := h.lookupHandshake(ctx)
	if err != nil {
		logQueryError("onboarding options", err)
		return internalError[api.GetOnboardingOptionsResponseObject]()
	}
	if !hs.live {
		return api.GetOnboardingOptions401JSONResponse{
			OnboardingUnauthorizedJSONResponse: h.onboardingUnauthorized(hs.presented),
		}, nil
	}
	return api.GetOnboardingOptions200JSONResponse{
		Email:       hs.row.Email,
		DisplayName: hs.row.DisplayName,
		// Display only: the commit enforces it.
		FoundingOpen: !h.foundingDisabled,
	}, nil
}

// CommitOnboardingChoice founds the Household the handshake's holder chose to
// found. Refusals that need no claim (a dead handshake, a body that is not a
// found choice, founding disabled, a bad name) are answered without spending
// the handshake, so a typo in the name costs the person nothing.
//
// The commit is one transaction: claim the handshake with DELETE ... RETURNING,
// insert the Household, the User and the credential, commit; only then issue
// the session. Balances runs these as separate statements with no transaction,
// so a double submit there leaves an orphan Household. Here two concurrent
// commits on one handshake serialise on its row: the second finds it gone and
// answers 401, and nothing of it was written.
func (h *Handlers) CommitOnboardingChoice(ctx context.Context, request api.CommitOnboardingChoiceRequestObject) (api.CommitOnboardingChoiceResponseObject, error) {
	budget, hasBudget := handlerBudget(ctx)
	body := request.Body

	hs, err := h.lookupHandshake(ctx)
	if err != nil {
		logQueryError("onboarding choice", err)
		return internalError[api.CommitOnboardingChoiceResponseObject]()
	}
	if !hs.live {
		return api.CommitOnboardingChoice401JSONResponse{
			OnboardingUnauthorizedJSONResponse: h.onboardingUnauthorized(hs.presented),
		}, nil
	}

	if !body.Found {
		// Not a found choice. A join choice arrives with invitations, later.
		return invalidChoiceField("found", "required"), nil
	}
	if h.foundingDisabled {
		return api.CommitOnboardingChoice403JSONResponse{
			ForbiddenJSONResponse: api.ForbiddenJSONResponse{Code: api.FOUNDINGDISABLED},
		}, nil
	}
	householdName := strings.TrimSpace(body.HouseholdName)
	switch {
	case householdName == "":
		return invalidChoiceField("household_name", "required"), nil
	case utf8.RuneCountInString(householdName) > maxHouseholdNameCodePoints:
		return invalidChoiceField("household_name", "max"), nil
	case !storable(householdName):
		return invalidChoiceField("household_name", "pattern"), nil
	case invisible(householdName):
		// Only zero-width or other format characters: blank to the eye.
		return invalidChoiceField("household_name", "required"), nil
	}

	// A person who commits gets an account even if their client then goes away:
	// the commit must not be a coin flip between "created" and "rolled back"
	// that the client never learns the outcome of. Detached from the request's
	// cancellation, bounded by the handler timeout again, as login's tail is
	// (#33).
	ctx, cancel := afterPermit(ctx, budget, hasBudget)
	defer cancel()

	userID, err := h.found(ctx, onboardingTokenFrom(ctx), householdName)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Claimed by a concurrent commit, or expired since the read above.
		return api.CommitOnboardingChoice401JSONResponse{
			OnboardingUnauthorizedJSONResponse: h.onboardingUnauthorized(true),
		}, nil
	case isEmailTaken(err):
		// Taken between register and commit. The transaction rolled back, so
		// nothing was created.
		return api.CommitOnboardingChoice409JSONResponse{
			ConflictJSONResponse: api.ConflictJSONResponse{Code: api.EMAILTAKEN},
		}, nil
	case err != nil:
		logQueryError("onboarding choice: found household", err)
		return internalError[api.CommitOnboardingChoiceResponseObject]()
	}

	token, expiresAt, err := h.createSession(ctx, userID, userAgentFrom(ctx))
	if err != nil {
		// The account exists and the handshake is spent, so there is nothing to
		// retry here: the person signs in with the password they just chose.
		slog.Error("onboarding choice: issue session", "err", err)
		return internalError[api.CommitOnboardingChoiceResponseObject]()
	}
	return commitSucceeded{h: h, token: token, expires: expiresAt}, nil
}

// found runs the whole commit in one transaction and returns the new User's id.
// pgx.ErrNoRows means the handshake was not there to claim.
func (h *Handlers) found(ctx context.Context, token, householdName string) (pgtype.UUID, error) {
	tx, err := h.tx.Begin(ctx)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("begin: %w", err)
	}
	// A no-op after Commit. Every other return rolls back, the claim included,
	// so a failed founding leaves the handshake as it found it.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := db.New(tx)

	hs, err := q.ClaimOnboardingHandshake(ctx, HashToken(token))
	if err != nil {
		return pgtype.UUID{}, err
	}

	householdID, err := newUUIDv7()
	if err != nil {
		return pgtype.UUID{}, err
	}
	userID, err := newUUIDv7()
	if err != nil {
		return pgtype.UUID{}, err
	}

	if _, err := q.CreateHousehold(ctx, db.CreateHouseholdParams{ID: householdID, DisplayName: householdName}); err != nil {
		return pgtype.UUID{}, fmt.Errorf("create household: %w", err)
	}
	if _, err := q.CreateUser(ctx, db.CreateUserParams{
		ID:          userID,
		HouseholdID: householdID,
		Email:       hs.Email,
		DisplayName: hs.DisplayName,
		Locale:      hs.Locale,
	}); err != nil {
		return pgtype.UUID{}, fmt.Errorf("create user: %w", err)
	}
	// Nullable for a Google handshake, which has no password to carry over.
	if hs.PasswordHash != nil {
		if err := q.CreateCredential(ctx, db.CreateCredentialParams{UserID: userID, PasswordHash: *hs.PasswordHash}); err != nil {
			return pgtype.UUID{}, fmt.Errorf("create credential: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return pgtype.UUID{}, fmt.Errorf("commit: %w", err)
	}
	return userID, nil
}

// isEmailTaken reports the one unique violation a founding can hit that is not
// a bug: the address registered earlier has since been taken.
func isEmailTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == usersEmailIndex
}

// newUUIDv7 mints a primary key. Founding is an online auth flow, so the server
// generates these, where a domain POST takes the client's (BOOTSTRAP.md §4).
func newUUIDv7() (pgtype.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("generate uuid: %w", err)
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

func invalidChoiceField(field, rule string) api.CommitOnboardingChoiceResponseObject {
	return api.CommitOnboardingChoice400JSONResponse{
		BadRequestJSONResponse: api.BadRequestJSONResponse{
			Code: api.VALIDATION,
			Args: &map[string]interface{}{"field": field, "rule": rule},
		},
	}
}

// commitSucceeded is the commit's 204: the session cookie, then the handshake
// cookie expired. Not the generated CommitOnboardingChoice204Response, because
// OpenAPI 3.0 describes one value per response header and the generated code
// writes it with Header().Set, so a second Set-Cookie line would replace the
// first. It satisfies the generated response interface, so the handler still
// cannot return a status the contract does not declare.
//
// Both cookies are built here, where they are written, rather than carried in
// as *http.Cookie fields: CodeQL cannot follow a cookie through a struct field
// into this interface method, and flagged HttpOnly and Secure as unset (#109).
type commitSucceeded struct {
	h       *Handlers
	token   string
	expires time.Time
}

func (r commitSucceeded) VisitCommitOnboardingChoiceResponse(w http.ResponseWriter) error {
	http.SetCookie(w, r.h.sessionCookie(r.token, r.expires))
	http.SetCookie(w, r.h.ClearedOnboardingCookie())
	w.WriteHeader(http.StatusNoContent)
	return nil
}
