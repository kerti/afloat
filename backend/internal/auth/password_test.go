package auth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestHashPasswordProducesPHCWithPinnedParameters(t *testing.T) {
	phc, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	// The exact prefix matters: BOOTSTRAP.md §5.1 requires a hash written here
	// to verify in the Kotlin backend, which reads these parameters out of the
	// string. A silent retune would break that across backends.
	wantPrefix := "$argon2id$v=19$m=19456,t=2,p=1$"
	if !strings.HasPrefix(phc, wantPrefix) {
		t.Errorf("hash = %q, want prefix %q", phc, wantPrefix)
	}
	if got := len(strings.Split(phc, "$")); got != 6 {
		t.Errorf("hash has %d $-separated parts, want 6", got)
	}
}

func TestHashPasswordSaltsEveryHash(t *testing.T) {
	a, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	b, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if a == b {
		t.Error("two hashes of one password are identical; the salt is not random")
	}
}

func TestVerifyPassword(t *testing.T) {
	phc, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !VerifyPassword("correct horse battery staple", phc) {
		t.Error("the correct password did not verify")
	}
	if VerifyPassword("Correct horse battery staple", phc) {
		t.Error("a differently-cased password verified")
	}
	if VerifyPassword("", phc) {
		t.Error("an empty password verified")
	}
}

// A corrupt row must fail the login, not crash the handler.
func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for _, phc := range []string{
		"",
		"not a phc string",
		"$argon2id$v=19$m=19456,t=2,p=1$onlyfourparts",
		"$bcrypt$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=99$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=bad,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$!!!notbase64$aGFzaA",
	} {
		if VerifyPassword("anything", phc) {
			t.Errorf("malformed hash verified: %q", phc)
		}
	}
}

// A hash carrying different cost parameters must still verify, or retuning the
// constants would lock every existing user out.
func TestVerifyPasswordHonoursTheHashesOwnParameters(t *testing.T) {
	const (
		otherMemory  = 8 * 1024
		otherTime    = 1
		otherThreads = 2
	)
	salt := []byte("sixteenbytesalt!")
	sum := argon2.IDKey([]byte("hunter2hunter2"), salt, otherTime, otherMemory, otherThreads, argonKeyLen)
	phc := buildPHC(otherMemory, otherTime, otherThreads, salt, sum)

	if !VerifyPassword("hunter2hunter2", phc) {
		t.Error("a hash with non-default parameters failed to verify")
	}
	if VerifyPassword("wrong password here", phc) {
		t.Error("a wrong password verified against non-default parameters")
	}
}

func TestValidatePasswordPolicy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
		wantErr  error
	}{
		{"at the floor", "1234567890a", nil},
		{"a real passphrase", "kucing oranye di atap", nil},
		{"one rune short", "123456789", ErrPasswordTooShort},
		{"empty", "", ErrPasswordTooShort},
		{"common", "password1234", ErrPasswordCommon},
		{"common, different case", "PassWord1234", ErrPasswordCommon},
		{"too long", strings.Repeat("a", maxPasswordLen+1), ErrPasswordTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidatePasswordPolicy(tc.password); got != tc.wantErr {
				t.Errorf("ValidatePasswordPolicy = %v, want %v", got, tc.wantErr)
			}
		})
	}
}

// Length is counted in runes, so a passphrase in a non-ASCII script is not
// penalised for encoding to more bytes than it has characters.
func TestPasswordLengthIsCountedInRunes(t *testing.T) {
	const tenRunes = "秘密の合言葉です" // 8 runes, 24 bytes
	if err := ValidatePasswordPolicy(tenRunes); err != ErrPasswordTooShort {
		t.Errorf("an 8-rune password was accepted on its byte length: %v", err)
	}
	if err := ValidatePasswordPolicy(tenRunes + "あい"); err != nil {
		t.Errorf("a 10-rune password was rejected: %v", err)
	}
}
