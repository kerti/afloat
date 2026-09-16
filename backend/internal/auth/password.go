package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
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
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	sum := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return buildPHC(argonMemoryKiB, argonTime, argonThreads, salt, sum), nil
}

// VerifyPassword checks a password against a stored PHC string, using that
// string's own recorded cost parameters rather than the constants above — so a
// retune does not invalidate existing hashes.
//
// Returns false rather than an error for a hash it cannot parse: a corrupt row
// must fail the login, not crash the handler, and the caller has no different
// action to take either way.
func VerifyPassword(password, phc string) bool {
	params, salt, want, err := parsePHC(phc)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(want)))
	// Constant time: a byte-wise comparison leaks how much of the hash matched.
	return subtle.ConstantTimeCompare(got, want) == 1
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
