package httpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/system"
)

func newLocalDisabledServer(q *spyQuerier) http.Handler {
	return New(Deps{
		System: system.New(system.Deps{Querier: q, Version: "test", LocalEnabled: false}),
		Auth: auth.New(auth.Deps{
			Querier:            q,
			SessionTTL:         30 * 24 * time.Hour,
			SessionMaxLifetime: 90 * 24 * time.Hour,
			CookieSecure:       true,
		}),
		HandlerTimeout: testHandlerTimeout,
	})
}

// answer is what a client sees on the wire, framing included.
type answer struct {
	status        int
	body          string
	contentLength int64
	headers       map[string]string
}

// ask sends one request to a live server, not a ResponseRecorder: Content-Length
// and chunking on a HEAD are decided by net/http after the handler returns, so
// only the wire shows them.
func ask(t *testing.T, base, method, path, body string) answer {
	t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)

	a := answer{status: resp.StatusCode, body: string(raw), contentLength: resp.ContentLength, headers: map[string]string{}}
	for _, h := range []string{"Content-Type", "Allow", "Set-Cookie", "Retry-After", "Transfer-Encoding", "Cache-Control", "X-Content-Type-Options"} {
		a.headers[h] = strings.Join(resp.Header.Values(h), ",")
	}
	if len(resp.TransferEncoding) > 0 {
		a.headers["Transfer-Encoding"] = strings.Join(resp.TransferEncoding, ",")
	}
	return a
}

// With the local provider off, every route of it answers like a path that was
// never registered, on every method (#24, #66): the gate cannot be told from a
// missing route by status, body, headers or framing. Compared against the
// server's own answer for the same method on a path that does not exist, so
// the assertion is a relation, not a hard-coded 404.
//
// spyQuerier panics on any query it does not stub, so a request that reached a
// handler fails loudly; it records a session too, which none may create.
func TestEveryDisabledLocalRouteIsIndistinguishableFromAnUnregisteredPath(t *testing.T) {
	body := `{"email":"a@example.com","password":"correct horse battery staple"}`
	paths := []string{
		localLoginPath,
		localRegisterPath,
		localChangePasswordPath,
		localChangeEmailPath,
		// An encoded spelling is the same route once decoded (#56), so the gate
		// holds on it too.
		"/api/auth/local/regist%65r",
		"/api/auth/local/r%65gister",
		"/api/auth/local/change-%70assword",
		"/api/auth/local/ch%61nge-email",
	}
	methods := []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodTrace, "BREW", "PROPFIND",
	}

	q := &spyQuerier{}
	ts := httptest.NewServer(newLocalDisabledServer(q))
	defer ts.Close()

	for _, path := range paths {
		for _, method := range methods {
			t.Run(method+" "+path, func(t *testing.T) {
				got := ask(t, ts.URL, method, path, body)
				want := ask(t, ts.URL, method, "/api/no-such-route", body)

				if got.status != want.status {
					t.Errorf("status = %d, an unregistered path answers %d", got.status, want.status)
				}
				if got.body != want.body {
					t.Errorf("body = %q, an unregistered path answers %q", got.body, want.body)
				}
				if got.contentLength != want.contentLength {
					t.Errorf("Content-Length = %d, an unregistered path answers %d", got.contentLength, want.contentLength)
				}
				for h, v := range want.headers {
					if got.headers[h] != v {
						t.Errorf("%s = %q, an unregistered path answers %q", h, got.headers[h], v)
					}
				}
			})
		}
	}
	if q.sessionCreated {
		t.Error("a session was created though the routes should not exist")
	}
}

// The mirror: with the provider on the gate is a no-op, so register exists.
// A request that does not decode is 400 from the contract's validation before
// any handler runs.
func TestLocalRegisterEnabledIsMounted(t *testing.T) {
	srv := newTestServer()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, localRegisterPath, nil))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (no body) rather than 404", rec.Code)
	}
}
