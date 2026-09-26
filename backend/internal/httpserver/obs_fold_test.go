package httpserver

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/system"
	"github.com/kerti/afloat/backend/internal/testutil"
)

// #70's obs-fold ruling: Go and Tomcat unfold an obsolete header fold (RFC
// 7230 §3.2.4) differently, so the row a login leaves behind used to depend on
// which backend answered it. This cannot be driven through the conformance
// harness - net/http's own client refuses to put a raw CR or LF in a header
// value - so it needs a raw socket against a real server and a real database,
// the same way ErrorDispatchSpec.kt's Kotlin equivalent does.
func TestObsFoldUserAgentConvergesInTheStoredRow(t *testing.T) {
	tdb := testutil.NewTestDB(t)

	srv := New(Deps{
		System: system.New(system.Deps{Querier: tdb.Queries, Version: "test", LocalEnabled: true}),
		Auth: auth.New(auth.Deps{
			Querier:            tdb.Queries,
			Beginner:           tdb.Pool,
			SessionTTL:         30 * 24 * time.Hour,
			SessionMaxLifetime: 90 * 24 * time.Hour,
			CookieSecure:       true,
		}),
		HandlerTimeout: 10 * time.Second,
	})
	httpSrv := &http.Server{Handler: srv}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = httpSrv.Serve(ln) }()
	t.Cleanup(func() { _ = httpSrv.Close() })

	const password = "correct horse battery staple"

	for i, tc := range []struct {
		name string
		// fold is the literal bytes placed as the User-Agent header's value,
		// obs-fold CRLF included - written directly into the raw request, so
		// what reaches the connector is exactly these bytes, not a Go string
		// re-escaped by anything that thinks it owns header framing.
		fold string
		want *string
	}{
		{"an internal fold with a tab converges to a single space", "abc\r\n\tdef", strPtr("abc def")},
		{"a leading fold with nothing before it converges to no leading space", "\r\n def", strPtr("def")},
		{"a trailing fold with nothing after it converges to no trailing space", "abc\r\n ", strPtr("abc")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			email := fmt.Sprintf("obsfold%d@example.com", i)
			householdID := tdb.CreateHousehold(t, "Obs-fold Household")
			userID := tdb.CreateUser(t, householdID, email, "Obs Fold")
			hash, err := auth.HashPassword(context.Background(), password)
			if err != nil {
				t.Fatalf("HashPassword: %v", err)
			}
			if err := tdb.Queries.UpsertCredential(context.Background(), db.UpsertCredentialParams{
				UserID:       userID,
				PasswordHash: hash,
			}); err != nil {
				t.Fatalf("UpsertCredential: %v", err)
			}

			body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
			raw := "POST /api/auth/local/login HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"Content-Type: application/json\r\n" +
				"Content-Length: " + strconv.Itoa(len(body)) + "\r\n" +
				"User-Agent: " + tc.fold + "\r\n" +
				"Connection: close\r\n\r\n" + body

			conn, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			if _, err := conn.Write([]byte(raw)); err != nil {
				t.Fatal(err)
			}
			answer, err := io.ReadAll(conn)
			if err != nil {
				t.Fatal(err)
			}
			statusLine, _, _ := strings.Cut(string(answer), "\r\n")
			if statusLine != "HTTP/1.1 204 No Content" {
				t.Fatalf("login status line = %q, want 204 (answer: %q)", statusLine, answer)
			}

			var got *string
			err = tdb.Pool.QueryRow(context.Background(),
				`SELECT user_agent FROM sessions WHERE user_id = $1`, userID).Scan(&got)
			if err != nil {
				t.Fatalf("query stored user_agent: %v", err)
			}
			if (got == nil) != (tc.want == nil) || (got != nil && tc.want != nil && *got != *tc.want) {
				t.Errorf("stored user_agent = %v, want %v", derefOrNil(got), derefOrNil(tc.want))
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func derefOrNil(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
