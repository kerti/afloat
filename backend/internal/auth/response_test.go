package auth

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kerti/afloat/backend/internal/api"
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
