package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters, fixed by BOOTSTRAP.md §5.1 and identical to Balances, so
// a household running both apps gets the same password handling — and a hash
// written by either backend verifies in the other.
//
// The values are encoded into every PHC string stored, so they can be retuned
// later with no migration and old hashes keep verifying against their own
// recorded cost.
const (
	argonMemoryKiB = 19 * 1024 // 19 MiB, OWASP's current floor
	argonTime      = 2
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16
	argonAlgo      = "argon2id"
)

// argonConcurrencyCap bounds how many Argon2id hashes may run at once,
// process-wide (#33, BOOTSTRAP.md §5.1). Each hash holds argonMemoryKiB
// live for its duration, and that memory is allocated per call, not once —
// so with no bound, an unauthenticated caller sending concurrent logins
// chooses the process's peak memory. At this cap: 4 × 19 MiB ≈ 76 MiB.
//
// Fixed by ruling, not an operator knob (BOOTSTRAP.md §12 stays untouched):
// retuning it means changing this constant, which keeps the memory ceiling
// arithmetic in one place next to the parameters it depends on rather than
// letting a deployment drift it silently.
const argonConcurrencyCap = 4

// argonSem is the semaphore every Argon2id call — real or dummy-cost-equalizer
// — must hold for its duration. A request beyond the cap queues for a permit
// rather than failing fast: an immediate 429 here would leak that the server
// is busy, which the login path's constant-work design treats as worth
// hiding, and queueing is the shape that preserves it (#33 ruling).
var argonSem = make(chan struct{}, argonConcurrencyCap)

// argonInFlight counts hashes currently holding a permit, and
// argonPeakInFlight is the highest value it has ever reached. Both exist so a
// test can assert the concurrency bound directly — the actual peak, tracked
// exactly as it happens — rather than infer it from memory or wall-clock
// timing, either of which is what makes that kind of test flaky by machine.
var (
	argonInFlight     atomic.Int32
	argonPeakInFlight atomic.Int32
)

// acquireArgonSlot blocks until a permit is free or ctx is done, whichever
// comes first. Callers pass the incoming request's own context so a queued
// wait is bounded by the same deadline the rest of that request already
// answers to (chi's Timeout middleware, 30s) — deliberately not a new,
// separate timeout invented for this one step.
func acquireArgonSlot(ctx context.Context) error {
	select {
	case argonSem <- struct{}{}:
		n := argonInFlight.Add(1)
		for {
			peak := argonPeakInFlight.Load()
			if n <= peak || argonPeakInFlight.CompareAndSwap(peak, n) {
				break
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseArgonSlot() {
	argonInFlight.Add(-1)
	<-argonSem
}

// ArgonConcurrencyCapForTest reports the fixed cap. Exported for tests in
// other packages; nothing in production calls it, and there is deliberately
// no setter — the cap is a fixed constant (#33 ruling), not something even a
// test may change.
func ArgonConcurrencyCapForTest() int32 {
	return argonConcurrencyCap
}

// ArgonPeakInFlightForTest reports the highest number of Argon2 calls ever
// concurrently holding a permit since the last reset. Exported for tests in
// other packages; nothing in production calls it.
func ArgonPeakInFlightForTest() int32 {
	return argonPeakInFlight.Load()
}

// ResetArgonPeakInFlightForTest zeroes the value ArgonPeakInFlightForTest
// reports, so a test measures only the load it generates itself. Exported for
// tests in other packages; nothing in production calls it.
func ResetArgonPeakInFlightForTest() {
	argonPeakInFlight.Store(0)
}

// A floor and a denylist, no composition rules: forced symbols push people
// towards predictable substitutions, and length is what actually helps.
const (
	minPasswordLen = 10
	// Argon2id's cost does not grow with password length, so this is not about
	// hashing time — it bounds what gets read and held in memory.
	maxPasswordLen = 1024
)

var (
	ErrPasswordTooShort = errors.New("password is shorter than the minimum")
	ErrPasswordTooLong  = errors.New("password is longer than the maximum")
	ErrPasswordCommon   = errors.New("password is among commonly-breached passwords")
)

// ValidatePasswordPolicy reports whether a password may be set. Length is
// counted in runes: a passphrase in Indonesian or any non-ASCII script must not
// be penalised for encoding to more bytes.
func ValidatePasswordPolicy(password string) error {
	if utf8.RuneCountInString(password) < minPasswordLen {
		return ErrPasswordTooShort
	}
	if utf8.RuneCountInString(password) > maxPasswordLen {
		return ErrPasswordTooLong
	}
	if isCommonPassword(password) {
		return ErrPasswordCommon
	}
	return nil
}

// HashPassword returns a PHC string: $argon2id$v=19$m=...,t=...,p=...$salt$hash.
//
// Waits for an Argon2 permit under ctx first (#33) — the same bound
// VerifyPassword observes, so a future caller on the hot path (e.g.
// registration) cannot add to peak concurrent Argon2 work outside the cap.
func HashPassword(ctx context.Context, password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	if err := acquireArgonSlot(ctx); err != nil {
		return "", fmt.Errorf("wait for argon2 slot: %w", err)
	}
	defer releaseArgonSlot()
	sum := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return buildPHC(argonMemoryKiB, argonTime, argonThreads, salt, sum), nil
}

// VerifyPassword checks a password against a stored PHC string, using that
// string's own recorded cost parameters rather than the constants above — so a
// retune does not invalidate existing hashes.
//
// Returns (false, nil) rather than an error for a hash it cannot parse: a
// corrupt row must fail the login, not crash the handler, and the caller has
// no different action to take either way. A non-nil error means the call
// never ran the hash at all — ctx ended while queued for an Argon2 permit —
// which the caller must not treat the same as a wrong password (#33).
func VerifyPassword(ctx context.Context, password, phc string) (bool, error) {
	params, salt, want, err := parsePHC(phc)
	if err != nil {
		return false, nil
	}
	if err := acquireArgonSlot(ctx); err != nil {
		return false, err
	}
	defer releaseArgonSlot()
	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(want)))
	// Constant time: a byte-wise comparison leaks how much of the hash matched.
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func parsePHC(phc string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(phc, "$")
	// ["", algo, v=..., m=...,t=...,p=..., salt, hash]
	if len(parts) != 6 {
		return argonParams{}, nil, nil, errors.New("malformed PHC string")
	}
	if parts[1] != argonAlgo {
		return argonParams{}, nil, nil, fmt.Errorf("unsupported algorithm %q", parts[1])
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("parse version: %w", err)
	}
	if version != argon2.Version {
		return argonParams{}, nil, nil, fmt.Errorf("unsupported argon2 version %d", version)
	}

	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("parse parameters: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("decode salt: %w", err)
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("decode hash: %w", err)
	}
	return p, salt, hash, nil
}

// buildPHC assembles the stored representation. Split out so tests can build a
// hash with parameters other than the current constants.
func buildPHC(memory, time uint32, threads uint8, salt, sum []byte) string {
	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argonAlgo, argon2.Version, memory, time, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum),
	)
}
