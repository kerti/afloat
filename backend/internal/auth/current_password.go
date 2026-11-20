package auth

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// The two operations that need the current password, a password change and an
// email change, prove it through one function and one backoff key, so the two
// routes cannot double the guesses a stolen session gets.

// currentPasswordKeys is the backoff key for a User's current-password
// guesses (BOOTSTRAP.md §5.1). It is keyed on the User, not on the address or
// the IP, and the reasons are the point:
//
//   - login's email: key would let a stolen session throttle the owner's
//     sign-in, and the owner's own typos here would throttle sign-in on every
//     device;
//   - an ip: component depends on #110 (behind a reverse proxy every visitor
//     shares one), and the guesser is already inside a session anyway.
//
// It adds at most one login_attempts row per User, and success clears it.
func currentPasswordKeys(userID pgtype.UUID) []string {
	return []string{"current-password:" + uuid.UUID(userID.Bytes).String()}
}

type refusalKind int

// The zero refusal is no refusal: the current password verified.
const (
	// refusedThrottled: inside the backoff window; refusal.retryAfter is set.
	refusedThrottled refusalKind = iota + 1
	// refusedWrong: the password did not verify, or there was nothing to verify
	// it against. One answer for both, as login's.
	refusedWrong
	// refusedInternal: a failure that says nothing about the password. Already
	// logged, and nothing was recorded against the key.
	refusedInternal
)

type refusal struct {
	kind       refusalKind
	retryAfter int
}

// passwordProof is a verified current password, and what the caller needs to
// carry on from it: the Argon2 permit, still held so the new password can be
// hashed under it, and a context detached from the request.
type passwordProof struct {
	h *Handlers
	// ctx is detached from the request's cancellation and bounded by the
	// handler timeout again, counted from the permit (afterPermit). Everything
	// after the verify runs on it: a caller whose password checked out gets its
	// write, whether or not their client has gone (#33).
	ctx context.Context
	// hash is the stored hash the password verified against, which a password
	// change compare-and-swaps on.
	hash string
	keys []string

	releasePermit func()
	cancel        context.CancelFunc
}

// release gives the Argon2 permit back. Safe to call more than once.
func (p *passwordProof) release() { p.releasePermit() }

// close gives back everything the proof holds, and is deferred by the caller.
func (p *passwordProof) close() {
	p.releasePermit()
	p.cancel()
}

// clear forgets the User's failed guesses. A stale row is a nuisance, not a
// reason to refuse a request that has already proved the password.
func (p *passwordProof) clear() {
	if err := p.h.q.ClearLoginAttempts(p.ctx, p.keys); err != nil {
		slog.Warn("current password: clear attempts", "err", err)
	}
}

// proveCurrentPassword runs the shared steps of BOOTSTRAP.md §5.1.1, after the
// caller's own shape and policy checks, in this order, each reachable only if
// the earlier ones passed:
//
//  1. the throttle is read before queueing, so a throttled caller is answered
//     without waiting for a permit;
//  2. the credential is read. Only pgx.ErrNoRows means there is none; any other
//     failure is an outage, answered 500 with no failure recorded (#23);
//  3. an Argon2 permit is acquired. The context ending while queued is a 500
//     and records nothing (#33);
//  4. detached from the request (afterPermit), the throttle is read again under
//     the permit, and the password is verified against the stored hash by the
//     parameters inside it. A mismatch records the failure before the permit
//     passes on, so one caller's failure throttles the next in the queue.
//
// A User with no credential row is verified against the dummy hash and so
// answered as a wrong password, at the same cost, as login's.
//
// On a nil proof, refusal says why. On success the caller must close the proof.
func (h *Handlers) proveCurrentPassword(ctx context.Context, op string, userID pgtype.UUID, password string) (*passwordProof, refusal) {
	keys := currentPasswordKeys(userID)
	budget, hasBudget := handlerBudget(ctx)

	wait, err := h.backoffRemaining(ctx, keys)
	if err != nil {
		logQueryError(op+": read backoff", err)
		return nil, refusal{kind: refusedInternal}
	}
	if wait > 0 {
		return nil, refusal{kind: refusedThrottled, retryAfter: retryAfterSeconds(wait)}
	}

	stored := dummyHash
	cred, err := h.q.GetCredentialByUserID(ctx, userID)
	switch {
	case err == nil:
		stored = cred.PasswordHash
	case !errors.Is(err, pgx.ErrNoRows):
		logQueryError(op+": look up credential", err)
		return nil, refusal{kind: refusedInternal}
	}

	if err := acquireArgonPermit(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Warn(op+": client left while waiting for an argon2 permit", "err", err)
		} else {
			slog.Error(op+": wait for argon2 permit", "err", err)
		}
		return nil, refusal{kind: refusedInternal}
	}
	releasePermit := sync.OnceFunc(releaseArgonPermit)
	ctx, cancel := afterPermit(ctx, budget, hasBudget)
	proof := &passwordProof{h: h, ctx: ctx, hash: stored, keys: keys, releasePermit: releasePermit, cancel: cancel}

	wait, err = h.backoffRemaining(ctx, keys)
	if err != nil {
		// Detached, so never a client leaving.
		slog.Error(op+": read backoff", "err", err)
		proof.close()
		return nil, refusal{kind: refusedInternal}
	}
	if wait > 0 {
		proof.close()
		return nil, refusal{kind: refusedThrottled, retryAfter: retryAfterSeconds(wait)}
	}
	if !verifyHoldingPermit(password, stored) {
		h.recordFailure(ctx, keys)
		proof.close()
		return nil, refusal{kind: refusedWrong}
	}
	return proof, refusal{}
}
