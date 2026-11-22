package httpserver

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/system"
)

// oapi-codegen binds path parameters BEFORE the per-operation middlewares run,
// so a malformed {id} reaches paramErrorHandlerFor before sessionRequired can
// speak. BOOTSTRAP.md §6 says a caller without a session learns nothing about
// what the validator would have said, and the sweeps beside this one fill a
// VALID uuid, so they cannot see the gap: this fills one that does not bind
// (#121, P10). Proven red against a paramErrorHandler that did not ask for the
// session first: an unauthenticated PATCH /pockets/not-a-uuid answered 400.
func TestEveryOperationWithAPathParameterAnswersTheSessionBeforeABadParameter(t *testing.T) {
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatalf("load contract: %v", err)
	}
	outage := errors.New("connection reset by peer")

	for name, tc := range map[string]struct {
		q           sessionStub
		cookie      bool
		wantStatus  int
		wantCode    string
		wantCleared bool
	}{
		"no cookie":             {q: sessionStub{sessionErr: outage}, wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
		"dead session":          {q: sessionStub{sessionErr: pgx.ErrNoRows}, cookie: true, wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED", wantCleared: true},
		"session without user":  {q: sessionStub{userErr: pgx.ErrNoRows}, cookie: true, wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED", wantCleared: true},
		"session lookup outage": {q: sessionStub{sessionErr: outage}, cookie: true, wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL"},
		"user lookup outage":    {q: sessionStub{userErr: outage}, cookie: true, wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := New(Deps{
				System: system.New(system.Deps{Querier: tc.q, Version: "test", LocalEnabled: true}),
				Auth: auth.New(auth.Deps{
					Querier:            tc.q,
					SessionTTL:         30 * 24 * time.Hour,
					SessionMaxLifetime: 90 * 24 * time.Hour,
					CookieSecure:       true,
				}),
				HandlerTimeout: testHandlerTimeout,
			})

			swept := 0
			for path, item := range spec.Paths.Map() {
				if !strings.Contains(path, "{") {
					continue
				}
				for method, op := range item.Operations() {
					if !requiresSession(effectiveSecurity(spec, op)) {
						continue
					}
					swept++
					for _, bad := range []string{"not-a-uuid", "%2541", "a%2Fb"} {
						target := basePath + strings.NewReplacer("{id}", bad).Replace(path)
						req := httptest.NewRequest(method, target, strings.NewReader(`{"not":"valid"`))
						req.Header.Set("Sec-Fetch-Site", "same-origin")
						if op.RequestBody != nil {
							req.Header.Set("Content-Type", "application/json")
						}
						if tc.cookie {
							req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "a-token"})
						}
						rec := httptest.NewRecorder()
						srv.ServeHTTP(rec, req)

						if rec.Code != tc.wantStatus {
							t.Errorf("%s %s = %d, want %d (body %s)", method, target, rec.Code, tc.wantStatus, rec.Body.String())
							continue
						}
						assertEnvelope(t, rec, tc.wantCode)
						if rec.Header().Get("Set-Cookie") != "" != tc.wantCleared {
							t.Errorf("%s %s Set-Cookie = %q, want cleared=%v", method, target, rec.Header().Values("Set-Cookie"), tc.wantCleared)
						}
					}
				}
			}
			if swept == 0 {
				t.Fatal("no operation with a path parameter requires a session: this sweep checked nothing")
			}
		})
	}
}

// With a session, the parameter is reported as before: the 401 first must not
// swallow the 400.
func TestAPathParameterIsReportedToACallerWhoMayMakeTheRequest(t *testing.T) {
	f := newPocketFixture(t)
	rec := f.doAt(http.MethodPatch, "/api/pockets/not-a-uuid", `{"active":false}`)
	if rec.Code != http.StatusBadRequest || strings.TrimSpace(rec.Body.String()) != `{"code":"VALIDATION","args":{"field":"id","rule":"invalid"}}` {
		t.Errorf("answer = %d %s, want 400 VALIDATION {field: id, rule: invalid}", rec.Code, rec.Body.String())
	}
}
