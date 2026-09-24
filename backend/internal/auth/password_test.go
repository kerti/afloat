package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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

// #33: every Argon2 hash holds its 19 MiB for its duration, so the number in
// flight is the peak memory. Asserted on the counter taken with each permit,
// not on memory or elapsed time. Half the calls take the dummy-hash path an
// unknown address takes, which must share the one cap.
func TestVerifyPasswordBoundsConcurrentArgon2Calls(t *testing.T) {
	phc, err := HashPassword(context.Background(), "correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	argonPeakInFlight.Store(0)

	const n = 20
	var wg sync.WaitGroup
	results := make([]struct {
		ok  bool
		err error
	}, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			password, target := "correct horse battery staple", phc
			if i%2 == 1 {
				password, target = "wrong password entirely", dummyHash
			}
			results[i].ok, results[i].err = VerifyPassword(context.Background(), password, target)
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Errorf("call %d: unexpected error: %v", i, r.err)
		}
		if want := i%2 == 0; r.ok != want {
			t.Errorf("call %d: verified = %v, want %v", i, r.ok, want)
		}
	}
	// Exactly the cap: above it the bound failed, below it the burst never
	// contended and proved nothing.
	if got := argonPeakInFlight.Load(); got != argonConcurrencyCap {
		t.Errorf("peak concurrent Argon2 calls = %d, want %d", got, argonConcurrencyCap)
	}
}

// Queued past ctx, no password was checked: an error, never a false the
// caller would score as a wrong password. Every permit is held first, so the
// call waits and the deadline passes during the wait, not before it.
func TestArgonCallsReturnTheContextErrorWhenNoPermitComesFree(t *testing.T) {
	release := HoldArgonPermitsForTest()
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if ok, err := VerifyPassword(ctx, "wrong password entirely", dummyHash); !errors.Is(err, context.DeadlineExceeded) || ok {
		t.Errorf("VerifyPassword = (%v, %v), want (false, context.DeadlineExceeded)", ok, err)
	}
	if phc, err := HashPassword(ctx, "correct horse battery staple"); !errors.Is(err, context.DeadlineExceeded) || phc != "" {
		t.Errorf("HashPassword = (%q, %v), want (\"\", context.DeadlineExceeded)", phc, err)
	}
}

// A ctx that ended before the call never hashes, even with every permit free.
// Without the check ahead of the select, which picks a ready case at random,
// each call here would hash half the time.
func TestArgonCallsNeverHashForAnEndedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	argonPeakInFlight.Store(0)

	for range 20 {
		if ok, err := VerifyPassword(ctx, "wrong password entirely", dummyHash); !errors.Is(err, context.Canceled) || ok {
			t.Fatalf("VerifyPassword = (%v, %v), want (false, context.Canceled)", ok, err)
		}
		if phc, err := HashPassword(ctx, "correct horse battery staple"); !errors.Is(err, context.Canceled) || phc != "" {
			t.Fatalf("HashPassword = (%q, %v), want (\"\", context.Canceled)", phc, err)
		}
	}
	if got := argonPeakInFlight.Load(); got != 0 {
		t.Errorf("peak concurrent Argon2 calls = %d, want 0: a hash ran for an ended context", got)
	}
}
