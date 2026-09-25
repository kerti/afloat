package auth_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
)

// The login backoff both backends must implement alike
// (contract/testdata/login_backoff.json, issue #16). Kotlin's
// LoginBackoffFixtureSpec reads the same file.
type backoffFixture struct {
	Parameters struct {
		FirstBackoff string `json:"first_backoff"`
		MaxBackoff   string `json:"max_backoff"`
	} `json:"parameters"`
	Curve []struct {
		Failures       int     `json:"failures"`
		BackoffSeconds float64 `json:"backoff_seconds"`
	} `json:"curve"`
	Keys []struct {
		Email string   `json:"email"`
		IP    string   `json:"ip"`
		Keys  []string `json:"keys"`
	} `json:"keys"`
}

func loadBackoffFixture(t *testing.T) (backoffFixture, time.Duration, time.Duration) {
	t.Helper()
	raw, err := os.ReadFile("../../../contract/testdata/login_backoff.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx backoffFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(fx.Curve) == 0 || len(fx.Keys) == 0 {
		t.Fatal("fixture has an empty list: a fixture that asserts nothing looks like coverage")
	}
	first, err := time.ParseDuration(fx.Parameters.FirstBackoff)
	if err != nil {
		t.Fatalf("fixture first_backoff: %v", err)
	}
	maxBackoff, err := time.ParseDuration(fx.Parameters.MaxBackoff)
	if err != nil {
		t.Fatalf("fixture max_backoff: %v", err)
	}
	return fx, first, maxBackoff
}

// The curve is the SQL (queries/auth.sql, RecordLoginFailure), so the query is
// what gets driven, with the fixture's parameters: end to end, the second
// failure would be answered 429 by the window the first one set.
func TestBackoffCurveMatchesTheSharedFixture(t *testing.T) {
	fx, first, maxBackoff := loadBackoffFixture(t)
	if first != testFirstBackoff || maxBackoff != testMaxBackoff {
		t.Fatalf("the harness runs at %s/%s, the fixture at %s/%s", testFirstBackoff, testMaxBackoff, first, maxBackoff)
	}
	h := newHarness(t, time.Now)
	ctx := context.Background()
	const key = "email:curve@example.com"

	recorded := 0
	for _, step := range fx.Curve {
		if step.Failures <= recorded {
			t.Fatalf("curve rows must ascend: %d after %d", step.Failures, recorded)
		}
		for ; recorded < step.Failures; recorded++ {
			if err := h.tdb.Queries.RecordLoginFailure(ctx, db.RecordLoginFailureParams{
				Key:          key,
				FirstBackoff: pgtype.Interval{Microseconds: first.Microseconds(), Valid: true},
				MaxBackoff:   pgtype.Interval{Microseconds: maxBackoff.Microseconds(), Valid: true},
			}); err != nil {
				t.Fatalf("RecordLoginFailure, failure %d: %v", recorded+1, err)
			}
		}
		var failures int
		var window float64
		if err := h.tdb.Pool.QueryRow(ctx, `
			SELECT failure_count, extract(epoch FROM backoff_until - updated_at)::float8
			FROM login_attempts WHERE key = $1`, key).Scan(&failures, &window); err != nil {
			t.Fatalf("read row: %v", err)
		}
		if failures != step.Failures || window != step.BackoffSeconds {
			t.Errorf("after failure %d: count %d, window %gs; want count %d, window %gs",
				step.Failures, failures, window, step.Failures, step.BackoffSeconds)
		}
	}
}

// The keys one failed login writes, through the real request path: the
// middleware reads the connection's address, the handler normalises the email.
func TestBackoffKeysMatchTheSharedFixture(t *testing.T) {
	fx, _, _ := loadBackoffFixture(t)
	for _, c := range fx.Keys {
		t.Run(c.Email+" from "+c.IP, func(t *testing.T) {
			h := newHarness(t, time.Now)

			// Go's own RemoteAddr: host:port, IPv6 bracketed, as net/http sets it.
			req := httptest.NewRequest(http.MethodPost, "/api/auth/local/login", nil)
			req.RemoteAddr = net.JoinHostPort(c.IP, "54321")
			var ctx context.Context
			auth.RequestContextMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				ctx = r.Context()
			})).ServeHTTP(httptest.NewRecorder(), req)

			if _, is := h.login(ctx, t, c.Email, "not the password").(api.LocalLogin401JSONResponse); !is {
				t.Fatal("an unknown address must be refused as INVALID_CREDENTIALS")
			}

			rows, err := h.tdb.Pool.Query(context.Background(), `SELECT key FROM login_attempts ORDER BY key`)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			var got []string
			for rows.Next() {
				var k string
				if err := rows.Scan(&k); err != nil {
					t.Fatalf("scan: %v", err)
				}
				got = append(got, k)
			}
			want := slices.Sorted(slices.Values(c.Keys))
			if !slices.Equal(got, want) {
				t.Errorf("keys %q, want %q", got, want)
			}
		})
	}
}
