package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"testing"

	"golang.org/x/crypto/argon2"
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
		MintedBy string `json:"minted_by"`
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

// A reject row that tests a shape rule is worth something only while its tag is
// the one a lenient parser would compute: with any other tag it is refused by
// the mismatch, and the rule it names goes untested. So the Go-minted tags are
// recomputed here from whatever the string says, read as loosely as the old
// Sscanf parser read it. Kotlin's PasswordFixtureSpec does the same for the
// BouncyCastle-minted ones.
var lenientPHC = regexp.MustCompile(`^\$argon2id\$v=(\d+)\$m=(\d+),t=(\d+),p=(\d+)\$([^$]+)\$([^$]+)$`)

func TestArgon2FixtureGoMintedTagsAreGenuine(t *testing.T) {
	minted := 0
	for _, c := range loadArgon2Fixture(t).Rejects {
		if c.MintedBy != "go" {
			continue
		}
		minted++
		t.Run(c.Why, func(t *testing.T) {
			f := lenientPHC.FindStringSubmatch(c.PHC)
			if f == nil {
				t.Fatalf("not even loosely an argon2id PHC string: %q", c.PHC)
			}
			memory, _ := strconv.ParseUint(f[2], 10, 32)
			iterations, _ := strconv.ParseUint(f[3], 10, 32)
			threads, _ := strconv.ParseUint(f[4], 10, 8)
			salt, errS := base64.RawStdEncoding.DecodeString(f[5])
			tag, errH := base64.RawStdEncoding.DecodeString(f[6])
			if errS != nil || errH != nil {
				t.Fatalf("salt or tag does not decode: %q", c.PHC)
			}
			got := argon2.IDKey([]byte(c.Password), salt, uint32(iterations), uint32(memory), uint8(threads), uint32(len(tag)))
			if !bytes.Equal(got, tag) {
				t.Errorf("tag is not what x/crypto computes for these parameters, so only the mismatch refuses it: %q", c.PHC)
			}
		})
	}
	if minted == 0 {
		t.Fatal("no Go-minted reject rows: the shape rules they test have no genuine tag behind them")
	}
}
