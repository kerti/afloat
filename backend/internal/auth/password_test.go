package auth

import (
	"context"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestHashPasswordProducesPHCWithPinnedParameters(t *testing.T) {
	phc, err := HashPassword(context.Background(), "correct horse battery staple")
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
	a, err := HashPassword(context.Background(), "same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	b, err := HashPassword(context.Background(), "same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if a == b {
		t.Error("two hashes of one password are identical; the salt is not random")
	}
}

func TestVerifyPassword(t *testing.T) {
	phc, err := HashPassword(context.Background(), "correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if ok, err := VerifyPassword(context.Background(), "correct horse battery staple", phc); err != nil || !ok {
		t.Errorf("the correct password did not verify: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword(context.Background(), "Correct horse battery staple", phc); err != nil || ok {
		t.Errorf("a differently-cased password verified: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword(context.Background(), "", phc); err != nil || ok {
		t.Errorf("an empty password verified: ok=%v err=%v", ok, err)
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
		if ok, err := VerifyPassword(context.Background(), "anything", phc); err != nil || ok {
			t.Errorf("malformed hash verified: %q (ok=%v err=%v)", phc, ok, err)
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

	if ok, err := VerifyPassword(context.Background(), "hunter2hunter2", phc); err != nil || !ok {
		t.Errorf("a hash with non-default parameters failed to verify: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword(context.Background(), "wrong password here", phc); err != nil || ok {
		t.Errorf("a wrong password verified against non-default parameters: ok=%v err=%v", ok, err)
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

// The product bug from #33: unbounded concurrent Argon2 calls let an
// unauthenticated caller choose the process's peak memory. This proves the
// bound directly — argonPeakInFlight is updated exactly at the moment each
// call actually holds a permit (password.go), so the assertion below is the
// real peak, not an inference from memory or wall-clock timing, which is what
// made the equivalent Kotlin test flaky by machine (issue #33).
func TestVerifyPasswordBoundsConcurrentArgon2Calls(t *testing.T) {
	phc, err := HashPassword(context.Background(), "correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	// Isolate this test's peak from whatever earlier tests in this file left
	// behind — those run sequentially, so the true concurrent peak they could
	// have left is 1, but resetting keeps the assertion honest regardless.
	argonPeakInFlight.Store(0)

	const n = 20 // n >> argonConcurrencyCap, so the cap is what limits it, not n itself.

	var wg sync.WaitGroup
	results := make([]struct {
		ok  bool
		err error
	}, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// A mix: even indices check the real hash, odd ones the dummy-cost
			// path an unknown account takes — both must share the one cap.
			password, target := "correct horse battery staple", phc
			if i%2 == 1 {
				password, target = "wrong password entirely", dummyHash
			}
			ok, err := VerifyPassword(context.Background(), password, target)
			results[i].ok, results[i].err = ok, err
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Errorf("call %d: unexpected error (resource exhaustion, not a timeout): %v", i, r.err)
		}
	}
	for i := 0; i < n; i += 2 {
		if !results[i].ok {
			t.Errorf("call %d: correct password did not verify", i)
		}
	}
	for i := 1; i < n; i += 2 {
		if results[i].ok {
			t.Errorf("call %d: wrong password verified", i)
		}
	}

	if got := argonPeakInFlight.Load(); got > argonConcurrencyCap {
		t.Errorf("peak concurrent Argon2 calls = %d, want <= %d", got, argonConcurrencyCap)
	}
	if got := argonPeakInFlight.Load(); got != argonConcurrencyCap {
		// Not a hard requirement of the cap itself, but if 20 calls fired at
		// once never even reached the cap, the concurrency in this test setup
		// is not real and the assertion above proves nothing.
		t.Errorf("peak concurrent Argon2 calls = %d, want exactly %d (20 calls should have saturated the cap)", got, argonConcurrencyCap)
	}
}
