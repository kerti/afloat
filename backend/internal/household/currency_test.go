package household_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/kerti/afloat/backend/internal/httperr"
)

// contract/testdata/currency.json is what the settings picker offers. It must
// be exactly the set the server's validator accepts, or the picker offers a
// code the server then refuses (or hides one it would take).
func TestCurrencyFixtureMatchesValidator(t *testing.T) {
	raw, err := os.ReadFile("../../../contract/testdata/currency.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		Codes []string `json:"codes"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	if !slices.IsSorted(fx.Codes) {
		t.Error("fixture codes are not sorted")
	}
	v := httperr.Validator()
	for _, code := range fx.Codes {
		if v.Var(code, "iso4217") != nil {
			t.Errorf("fixture offers %s but the validator rejects it", code)
		}
	}
	for a := 'A'; a <= 'Z'; a++ {
		for b := 'A'; b <= 'Z'; b++ {
			for c := 'A'; c <= 'Z'; c++ {
				code := string([]rune{a, b, c})
				if v.Var(code, "iso4217") == nil && !slices.Contains(fx.Codes, code) {
					t.Errorf("validator accepts %s but the fixture lacks it", code)
				}
			}
		}
	}
}
