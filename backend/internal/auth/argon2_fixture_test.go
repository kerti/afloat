package auth

import (
	"encoding/json"
	"os"
	"testing"
)

// The PHC strings both backends must read alike (contract/testdata/argon2.json,
// issue #16). Kotlin's PasswordFixtureSpec reads the same file: a hash either
// backend wrote must verify in the other, and a string neither should accept
// must be refused by both - as "does not verify", never as a panic.
type argon2Fixture struct {
	Verifies []struct {
		MintedBy string `json:"minted_by"`
		Cost     string `json:"cost"`
		Password string `json:"password"`
		PHC      string `json:"phc"`
	} `json:"verifies"`
	Rejects []struct {
		Why      string `json:"why"`
		Password string `json:"password"`
		PHC      string `json:"phc"`
	} `json:"rejects"`
}

func loadArgon2Fixture(t *testing.T) argon2Fixture {
	t.Helper()
	raw, err := os.ReadFile("../../../contract/testdata/argon2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx argon2Fixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(fx.Verifies) == 0 || len(fx.Rejects) == 0 {
		t.Fatal("fixture has an empty list: a fixture that asserts nothing looks like coverage")
	}
	return fx
}

// verifies calls the verifier and turns a panic into a failure of its own, so
// a malformed string that crashes the process is reported as that, not as the
// test binary dying.
func verifies(t *testing.T, password, phc string) (ok bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("verifier panicked on %q: %v", phc, r)
			ok = false
		}
	}()
	return verifyHoldingPermit(password, phc)
}

func TestArgon2FixtureVerifies(t *testing.T) {
	for _, c := range loadArgon2Fixture(t).Verifies {
		t.Run(c.MintedBy+"/"+c.Cost+"/"+c.Password, func(t *testing.T) {
			if !verifies(t, c.Password, c.PHC) {
				t.Errorf("a %s-minted hash at cost %s does not verify its own password", c.MintedBy, c.Cost)
			}
		})
	}
}

func TestArgon2FixtureRejects(t *testing.T) {
	for _, c := range loadArgon2Fixture(t).Rejects {
		t.Run(c.Why, func(t *testing.T) {
			if verifies(t, c.Password, c.PHC) {
				t.Errorf("verified, want refused: %q", c.PHC)
			}
		})
	}
}
