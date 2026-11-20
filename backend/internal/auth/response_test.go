package auth

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/db"
)

// Each handler's unexpected-failure path must answer its own 500. A nil
// response object is written as nothing, which net/http sends as a 200.
func TestInternalErrorAnswersEachHandlersOwn500(t *testing.T) {
	login, err := internalError[api.LocalLoginResponseObject]()
	if _, is := login.(api.LocalLogin500JSONResponse); !is || err != nil {
		t.Errorf("login = (%T, %v), want (LocalLogin500JSONResponse, nil)", login, err)
	}
	me, err := internalError[api.GetMeResponseObject]()
	if _, is := me.(api.GetMe500JSONResponse); !is || err != nil {
		t.Errorf("me = (%T, %v), want (GetMe500JSONResponse, nil)", me, err)
	}
	logout, err := internalError[api.LogoutResponseObject]()
	if _, is := logout.(api.Logout500JSONResponse); !is || err != nil {
		t.Errorf("logout = (%T, %v), want (Logout500JSONResponse, nil)", logout, err)
	}
}

// internalError panics for a type its switch does not handle, and only on the
// error path that calls it, which no happy-path test reaches. So every
// internalError[api.X] in this package's source must name a type the switch
// handles. Both sides are read from the source, so a new handler is checked
// without anyone listing it here.
func TestEveryInternalErrorCallHasA500(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	handled := map[string]bool{}
	type call struct {
		typ string
		at  token.Position
	}
	var calls []call
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				if n.Name.Name != "internalError" {
					return true
				}
				ast.Inspect(n.Body, func(n ast.Node) bool {
					if c, ok := n.(*ast.CaseClause); ok {
						for _, e := range c.List {
							if star, ok := e.(*ast.StarExpr); ok {
								handled[apiTypeName(star.X)] = true
							}
						}
					}
					return true
				})
				return false
			case *ast.IndexExpr:
				if id, ok := n.X.(*ast.Ident); ok && id.Name == "internalError" {
					calls = append(calls, call{apiTypeName(n.Index), fset.Position(n.Pos())})
				}
			}
			return true
		})
	}

	if len(handled) == 0 || len(calls) == 0 {
		t.Fatalf("read %d handled types and %d calls; the source no longer has the shape this test reads", len(handled), len(calls))
	}
	for _, c := range calls {
		if !handled[c.typ] {
			t.Errorf("%s: internalError[%s] has no case in internalError's switch, so it panics", c.at, c.typ)
		}
	}
}

// apiTypeName renders api.X as "api.X", and anything else as "" (never handled).
func apiTypeName(e ast.Expr) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "api" {
		return ""
	}
	return "api." + sel.Sel.Name
}

// HouseholdResponse is the one projection of a Household, shared by GET /me and
// PATCH /household. Money leaves as a string at the storage scale, and a null
// income is not a field at all, so "none supplied" is never read as zero.
func TestHouseholdResponseRendersMoneyAtFourPlacesAndOmitsANull(t *testing.T) {
	base := db.Household{
		ID:                   pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		HouseholdDisplayName: "Home",
		ReportingCurrency:    "IDR",
		PeriodStartDay:       25,
		DayStartsAt:          pgtype.Time{Microseconds: (4*60 + 30) * 60 * 1_000_000, Valid: true},
		AllowanceMode:        "fixed",
	}

	for _, tc := range []struct {
		name   string
		income decimal.NullDecimal
		want   *string
	}{
		{"null is omitted", decimal.NullDecimal{}, nil},
		{"zero is a real answer", decimal.NullDecimal{Decimal: decimal.Zero, Valid: true}, ptr("0.0000")},
		{"whole number keeps its zeros", decimal.NullDecimal{Decimal: decimal.RequireFromString("25000000"), Valid: true}, ptr("25000000.0000")},
		{"fraction", decimal.NullDecimal{Decimal: decimal.RequireFromString("1234.56"), Valid: true}, ptr("1234.5600")},
		{"large value is not exponent notation", decimal.NullDecimal{Decimal: decimal.RequireFromString("100000000000000"), Valid: true}, ptr("100000000000000.0000")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := base
			h.ExpectedMonthlyIncome = tc.income
			got := HouseholdResponse(h)

			switch {
			case tc.want == nil && got.ExpectedMonthlyIncome != nil:
				t.Errorf("expected_monthly_income = %q, want it omitted", *got.ExpectedMonthlyIncome)
			case tc.want != nil && (got.ExpectedMonthlyIncome == nil || *got.ExpectedMonthlyIncome != *tc.want):
				t.Errorf("expected_monthly_income = %v, want %q", got.ExpectedMonthlyIncome, *tc.want)
			}
			if got.HouseholdDisplayName != "Home" || got.ReportingCurrency != "IDR" || got.PeriodStartDay != 25 ||
				got.DayStartsAt != "04:30" || got.AllowanceMode != api.Fixed {
				t.Errorf("projection = %+v", got)
			}
			wire, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var generic map[string]any
			if err := json.Unmarshal(wire, &generic); err != nil {
				t.Fatal(err)
			}
			switch v, present := generic["expected_monthly_income"]; {
			case tc.want == nil && present:
				t.Errorf("wire carries expected_monthly_income = %v, want no such key", v)
			case tc.want != nil && v != *tc.want:
				t.Errorf("wire expected_monthly_income = %#v, want the string %q", v, *tc.want)
			}
			if got.Id.String() != "01000000-0000-0000-0000-000000000000" {
				t.Errorf("id = %s", got.Id)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
