package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/system"
	"github.com/kerti/afloat/backend/internal/testutil"
)

func TestRequiresSession(t *testing.T) {
	session := openapi3.SecurityRequirement{"sessionCookie": {}}
	onboarding := openapi3.SecurityRequirement{"onboardingCookie": {}}
	for _, tc := range []struct {
		name string
		sec  openapi3.SecurityRequirements
		want bool
	}{
		{"none declared", nil, false},
		{"explicitly public", openapi3.SecurityRequirements{}, false},
		{"session only", openapi3.SecurityRequirements{session}, true},
		{"session or nothing", openapi3.SecurityRequirements{session, {}}, false},
		{"nothing or session", openapi3.SecurityRequirements{{}, session}, false},
		{"onboarding only", openapi3.SecurityRequirements{onboarding}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := requiresSession(tc.sec); got != tc.want {
				t.Errorf("requiresSession = %v, want %v", got, tc.want)
			}
		})
	}
}

// An operation's own security replaces the contract's global one; absent, it
// inherits.
func TestEffectiveSecurity(t *testing.T) {
	global := openapi3.SecurityRequirements{{"sessionCookie": {}}}
	spec := &openapi3.T{Security: global}

	if got := effectiveSecurity(spec, &openapi3.Operation{}); !requiresSession(got) {
		t.Error("an operation declaring nothing must inherit the global session requirement")
	}
	public := openapi3.SecurityRequirements{}
	if got := effectiveSecurity(spec, &openapi3.Operation{Security: &public}); requiresSession(got) {
		t.Error("`security: []` must opt an operation out of the global requirement")
	}
}

// Every operation the contract protects, with no session: 401 UNAUTHORIZED.
// Built from the contract the server runs, so an operation added later joins
// this sweep without anyone remembering to list it (#114).
func TestEveryOperationRequiringASessionRefusesWithoutOne(t *testing.T) {
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}
	srv := newTestServer()

	protected := 0
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			if !requiresSession(effectiveSecurity(spec, op)) {
				continue
			}
			protected++
			t.Run(method+" "+path, func(t *testing.T) {
				url := basePath + pathParamsFilled(path)
				var body *strings.Reader
				if op.RequestBody != nil {
					// Invalid for any schema: the 401 must come first.
					body = strings.NewReader(`{"not":"valid"`)
				} else {
					body = strings.NewReader("")
				}
				req := httptest.NewRequest(method, url, body)
				req.Header.Set("Sec-Fetch-Site", "same-origin")
				if op.RequestBody != nil {
					req.Header.Set("Content-Type", "application/json")
				}
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)

				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
				}
				assertEnvelope(t, rec, "UNAUTHORIZED")
			})
		}
	}
	if protected == 0 {
		t.Fatal("the contract has no operation requiring a session: this sweep checked nothing")
	}
}

func pathParamsFilled(path string) string {
	for {
		open := strings.IndexByte(path, '{')
		if open < 0 {
			return path
		}
		end := strings.IndexByte(path[open:], '}')
		path = path[:open] + "0192f0a0-0000-7000-8000-000000000001" + path[open+end+1:]
	}
}

// The byte-for-byte answer an unauthenticated /me gave when the handler wrote
// it: the conformance cases pin the same body.
func TestMeWithoutASessionAnswersTheSameBytes(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/me", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Body.String(); got != `{"code":"UNAUTHORIZED"}`+"\n" {
		t.Errorf("body = %q", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Errorf("Set-Cookie = %q, want none: nothing was presented", rec.Header().Get("Set-Cookie"))
	}
}

// HEAD is answered by the GET handler (headAsGet), so it is as protected.
func TestHeadOfAProtectedRouteWithoutASessionIsUnauthorized(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestServer().ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/api/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// Logout's contract says an empty alternative: no session is a success.
func TestLogoutWithoutASessionIsStillNoContent(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestServer().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("logout without a session must still clear the cookie")
	}
}

// Public operations take no part: they answer, or reach their own validation,
// without a session.
func TestPublicOperationsAreNotAskedForASession(t *testing.T) {
	srv := newTestServer()

	for _, path := range []string{"/api/health", "/api/auth/methods"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}

	// An invalid body is the validator's 400, so the request got past the
	// session check rather than being refused by it.
	for _, path := range []string{"/api/auth/local/login", "/api/auth/local/register"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s = %d, want 400 (body %s)", path, rec.Code, rec.Body.String())
		}
	}
}

// A route chi does not know is not the session check's to answer.
func TestUnknownRoutesKeepTheirNotFoundAndMethodNotAllowed(t *testing.T) {
	srv := newTestServer()
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/no-such-route", http.StatusNotFound},
		{http.MethodDelete, "/api/me", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/auth/logout", http.StatusMethodNotAllowed},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}

// deadHandshake answers every handshake lookup as an unknown token.
type deadHandshake struct{ db.Querier }

func (deadHandshake) GetLiveOnboardingHandshake(context.Context, string) (db.OnboardingHandshake, error) {
	return db.OnboardingHandshake{}, pgx.ErrNoRows
}

// onboardingCookie operations are the handler's to judge. A presented dead
// handshake clears its cookie, which only the handler's 401 does: the session
// check's 401 would not.
func TestOnboardingOperationsStayWithTheirHandlers(t *testing.T) {
	q := deadHandshake{}
	srv := New(Deps{
		System: system.New(system.Deps{Querier: q, Version: "test", LocalEnabled: true}),
		Auth: auth.New(auth.Deps{
			Querier:            q,
			SessionTTL:         30 * 24 * time.Hour,
			SessionMaxLifetime: 90 * 24 * time.Hour,
			CookieSecure:       true,
		}),
		HandlerTimeout: testHandlerTimeout,
	})

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/onboarding/options", ""},
		{http.MethodPost, "/api/onboarding/choice", `{"found":true,"household_name":"Home"}`},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Cookie", auth.OnboardingCookieName+"=stale")
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
			}
			assertEnvelope(t, rec, "UNAUTHORIZED")
			if c := rec.Header().Get("Set-Cookie"); !strings.Contains(c, auth.OnboardingCookieName+"=;") {
				t.Errorf("Set-Cookie = %q, want the handshake cookie cleared by the handler", c)
			}
		})
	}
}

// Without a cookie at all, they answer their own 401 too: no session needed to
// reach the gate, and nothing about the session check changes that.
func TestOnboardingOptionsWithoutAnyCookieIsTheHandlersUnauthorized(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestServer().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/onboarding/options", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertEnvelope(t, rec, "UNAUTHORIZED")
}

// The refusal comes before the validator speaks. No operation in the contract
// both needs a session and takes a body yet, so this puts a session
// requirement on a public operation that does, through the same chain mountAPI
// mounts: a 400 here would mean the validator ran first.
func TestSessionCheckRunsBeforeRequestValidation(t *testing.T) {
	r := chi.NewRouter()
	reached := false
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	})
	// The generated wrapper's own loop (api.gen.go).
	for _, mw := range operationMiddlewaresFor(map[string]bool{"POST /auth/local/login": true}) {
		handler = mw(handler)
	}
	r.Post(basePath+"/auth/local/login", handler.ServeHTTP)

	for name, body := range map[string]string{
		"malformed":     `{`,
		"fails a rule":  `{"email":"nope","password":""}`,
		"unknown field": `{"email":"a@example.com","password":"x","extra":1}`,
		"well formed":   `{"email":"a@example.com","password":"a valid password"}`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, basePath+"/auth/local/login", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
	if reached {
		t.Error("the handler ran for a request with no session")
	}
}

// A route the contract does not know is a fault of ours, never a silent pass.
func TestSessionCheckFailsClosedOnAnUnknownOperation(t *testing.T) {
	handler := sessionRequired(map[string]bool{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	r := chi.NewRouter()
	r.Get("/api/unlisted", handler.ServeHTTP)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/unlisted", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	assertEnvelope(t, rec, "INTERNAL")
}

// The other side of the guard, with a real session in a real database: a
// signed-in /me is still served.
func TestSignedInMeIsStillServed(t *testing.T) {
	tdb := testutil.NewTestDB(t)
	householdID := tdb.CreateHousehold(t, "Test Household")
	userID := tdb.CreateUser(t, householdID, "a@example.com", "Test User")

	const token = "a-session-token-for-the-signed-in-me-test"
	if _, err := tdb.Queries.CreateSession(context.Background(), db.CreateSessionParams{
		ID:        auth.HashToken(token),
		UserID:    userID,
		ExpiresAt: pgtypeTimestamptz(time.Now().Add(24 * time.Hour)),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	srv := New(Deps{
		System: system.New(system.Deps{Querier: tdb.Queries, Version: "test", LocalEnabled: true}),
		Auth: auth.New(auth.Deps{
			Querier:            tdb.Queries,
			Beginner:           tdb.Pool,
			SessionTTL:         30 * 24 * time.Hour,
			SessionMaxLifetime: 90 * 24 * time.Hour,
			CookieSecure:       true,
		}),
		HandlerTimeout: testHandlerTimeout,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Cookie", auth.SessionCookieName+"="+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"email":"a@example.com"`) {
		t.Errorf("body = %s, want the signed-in User", rec.Body.String())
	}
}

func pgtypeTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// A scheme the enforcement does not know would make its operation public with
// no warning, so building the requirements refuses it. So does an operation
// naming both cookies: the middleware cannot check a handshake.
func TestSessionRequirementsRefuseAnUnknownScheme(t *testing.T) {
	op := func(security openapi3.SecurityRequirements) *openapi3.Operation {
		return &openapi3.Operation{Security: &security}
	}
	for name, sec := range map[string]openapi3.SecurityRequirements{
		"a typo":                 {{"sessionCookies": {}}},
		"a new scheme":           {{"apiKey": {}}},
		"beside a known one":     {{"sessionCookie": {}}, {"bearer": {}}},
		"two in one alternative": {{"sessionCookie": {}, "bearer": {}}},
	} {
		t.Run(name, func(t *testing.T) {
			spec := &openapi3.T{Paths: openapi3.NewPaths()}
			spec.Paths.Set("/x", &openapi3.PathItem{Get: op(sec)})
			if _, err := sessionRequirements(spec); err == nil || !strings.Contains(err.Error(), "unknown security scheme") {
				t.Errorf("err = %v, want an unknown security scheme error", err)
			}
		})
	}

	for name, sec := range map[string]openapi3.SecurityRequirements{
		"session or onboarding":  {{"sessionCookie": {}}, {"onboardingCookie": {}}},
		"session and onboarding": {{"sessionCookie": {}, "onboardingCookie": {}}},
	} {
		t.Run(name, func(t *testing.T) {
			spec := &openapi3.T{Paths: openapi3.NewPaths()}
			spec.Paths.Set("/x", &openapi3.PathItem{Get: op(sec)})
			if _, err := sessionRequirements(spec); err == nil || !strings.Contains(err.Error(), "needs a ruling") {
				t.Errorf("err = %v, want the mixed-schemes refusal", err)
			}
		})
	}

	spec := &openapi3.T{Paths: openapi3.NewPaths()}
	spec.Paths.Set("/x", &openapi3.PathItem{Get: op(openapi3.SecurityRequirements{{"onboardingCookie": {}}})})
	if _, err := sessionRequirements(spec); err != nil {
		t.Errorf("a known scheme was refused: %v", err)
	}
}

// The contract the server embeds is itself well formed, and names only schemes
// the enforcement knows.
func TestEmbeddedContractIsValid(t *testing.T) {
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		t.Errorf("spec.Validate: %v", err)
	}
	if _, err := sessionRequirements(spec); err != nil {
		t.Errorf("sessionRequirements: %v", err)
	}
}

// Pins the operations that need no session, so one that becomes public by
// accident (a copy-pasted `security: []`) fails here instead of passing the
// sweep, which only covers the protected ones.
func TestTheUnprotectedOperationsArePinned(t *testing.T) {
	// Read from contract/openapi.yaml itself: the embedded copy's operationIds
	// are oapi-codegen's Go names (GetHealth), not the ones authors write.
	spec, err := openapi3.NewLoader().LoadFromFile("../../../contract/openapi.yaml")
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}
	want := []string{
		"commitOnboardingChoice",
		"getAuthMethods",
		"getHealth",
		"getOnboardingOptions",
		"localLogin",
		"localRegister",
		"logout",
	}

	var got []string
	for _, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			if !requiresSession(effectiveSecurity(spec, op)) {
				got = append(got, op.OperationID)
			}
		}
	}
	slices.Sort(got)

	if !slices.Equal(got, want) {
		t.Errorf("operations not requiring a session = %v, want %v\n"+
			"If this change is deliberate, update the list in this test; otherwise an operation has lost its session requirement.", got, want)
	}
}
