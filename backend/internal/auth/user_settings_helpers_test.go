package auth_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/db"
)

// Helpers shared by the User-settings tests: PATCH /me, change-password and
// change-email. Like the rest of this package they talk to a real Postgres and
// none is parallel (ADR-0004).

// member is a User with a credential, seeded into a Household.
type member struct {
	householdID, id pgtype.UUID
	email, password string
}

// seedMember adds a User with the given password to an existing Household.
func (h harness) seedMember(t *testing.T, householdID pgtype.UUID, email, password string) member {
	t.Helper()
	id := h.tdb.CreateUser(t, householdID, email, "Name of "+email)
	hash, err := auth.HashPassword(context.Background(), password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := h.tdb.Queries.UpsertCredential(context.Background(), db.UpsertCredentialParams{UserID: id, PasswordHash: hash}); err != nil {
		t.Fatalf("UpsertCredential: %v", err)
	}
	return member{householdID: householdID, id: id, email: email, password: password}
}

// seedMemberAlone is seedMember in a Household of its own.
func (h harness) seedMemberAlone(t *testing.T, email string) member {
	t.Helper()
	return h.seedMember(t, h.tdb.CreateHousehold(t, "Household of "+email), email, goodPassword)
}

// ctx is the context the real middleware gives a request signed in as m, from
// the User row as it stands now.
func (h harness) ctx(t *testing.T, m member, ip string) context.Context {
	t.Helper()
	user, err := h.tdb.Queries.GetUserByID(context.Background(), m.id)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	return auth.WithUser(auth.ContextForTest(context.Background(), ip, "Go-Test", ""), user)
}

// signIn opens a session for m and returns its plaintext token, as another
// device of theirs would hold it.
func (h harness) signIn(t *testing.T, m member) string {
	t.Helper()
	cookie, err := h.auth.IssueSession(context.Background(), m.id, "Go-Test")
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	return cookie.Value
}

// live reports whether a presented session token still resolves to a User.
func (h harness) live(t *testing.T, token string) bool {
	t.Helper()
	_, found, _ := h.resolve(t, token)
	return found
}

// snapshot is the rows a query selects, as one JSON document: every column,
// byte for byte, so "unchanged" means unchanged. query must select a single
// jsonb value.
func (h harness) snapshot(t *testing.T, query string, args ...any) string {
	t.Helper()
	var s string
	if err := h.tdb.Pool.QueryRow(context.Background(), query, args...).Scan(&s); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return s
}

func (h harness) userRow(t *testing.T, id pgtype.UUID) string {
	t.Helper()
	return h.snapshot(t, `SELECT to_jsonb(u)::text FROM users u WHERE id = $1`, id)
}

func (h harness) credentialRow(t *testing.T, userID pgtype.UUID) string {
	t.Helper()
	return h.snapshot(t, `SELECT coalesce(jsonb_agg(to_jsonb(c))::text, '[]') FROM credentials c WHERE user_id = $1`, userID)
}

func (h harness) sessionRows(t *testing.T, userID pgtype.UUID) string {
	t.Helper()
	return h.snapshot(t, `SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY id)::text, '[]') FROM sessions s WHERE user_id = $1`, userID)
}

// failureCount is the recorded failures under a backoff key; 0 with no row.
func (h harness) failureCount(t *testing.T, key string) int {
	t.Helper()
	var n int
	if err := h.tdb.Pool.QueryRow(context.Background(),
		`SELECT coalesce(max(failure_count), 0) FROM login_attempts WHERE key = $1`, key).Scan(&n); err != nil {
		t.Fatalf("failure count: %v", err)
	}
	return n
}

// currentPasswordKey is the backoff key of a User's current-password guesses.
func currentPasswordKey(m member) string {
	return "current-password:" + uuidString(m.id)
}

func uuidString(id pgtype.UUID) string {
	return id.String()
}

// expireBackoff moves every window into the past, so the next attempt is not
// throttled and the test need not sleep.
func (h harness) expireBackoff(t *testing.T) {
	t.Helper()
	if _, err := h.tdb.Pool.Exec(context.Background(),
		`UPDATE login_attempts SET backoff_until = now() - interval '1 second'`); err != nil {
		t.Fatalf("expire backoff: %v", err)
	}
}

func (h harness) changePassword(ctx context.Context, t *testing.T, current, next string) api.ChangeLocalPasswordResponseObject {
	t.Helper()
	resp, err := h.auth.ChangeLocalPassword(ctx, api.ChangeLocalPasswordRequestObject{
		Body: &api.ChangeLocalPasswordJSONRequestBody{CurrentPassword: current, NewPassword: next},
	})
	if err != nil {
		t.Fatalf("ChangeLocalPassword returned an error: %v", err)
	}
	return resp
}

// changedPassword is the new session's token from a 204.
func changedPassword(t *testing.T, resp api.ChangeLocalPasswordResponseObject) *http.Cookie {
	t.Helper()
	ok, is := resp.(api.ChangeLocalPassword204Response)
	if !is {
		t.Fatalf("response = %#v, want 204", resp)
	}
	if ok.Headers.SetCookie == nil {
		t.Fatal("204 carries no Set-Cookie")
	}
	c, err := http.ParseSetCookie(*ok.Headers.SetCookie)
	if err != nil {
		t.Fatalf("parse Set-Cookie: %v", err)
	}
	if c.Name != auth.SessionCookieName {
		t.Fatalf("cookie = %q, want %q", c.Name, auth.SessionCookieName)
	}
	return c
}

func (h harness) changeEmail(ctx context.Context, t *testing.T, email, current string) api.ChangeLocalEmailResponseObject {
	t.Helper()
	resp, err := h.auth.ChangeLocalEmail(ctx, api.ChangeLocalEmailRequestObject{
		Body: &api.ChangeLocalEmailJSONRequestBody{Email: email, CurrentPassword: current},
	})
	if err != nil {
		t.Fatalf("ChangeLocalEmail returned an error: %v", err)
	}
	return resp
}

func (h harness) updateMe(ctx context.Context, t *testing.T, body api.UpdateMeRequest) api.UpdateMeResponseObject {
	t.Helper()
	resp, err := h.auth.UpdateMe(ctx, api.UpdateMeRequestObject{Body: &body})
	if err != nil {
		t.Fatalf("UpdateMe returned an error: %v", err)
	}
	return resp
}

func (h harness) updatedMe(ctx context.Context, t *testing.T, body api.UpdateMeRequest) api.Me {
	t.Helper()
	resp := h.updateMe(ctx, t, body)
	me, is := resp.(api.UpdateMe200JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 200", resp)
	}
	return api.Me(me)
}

func assertUpdateMeValidation(t *testing.T, resp api.UpdateMeResponseObject, field, rule string) {
	t.Helper()
	got, is := resp.(api.UpdateMe400JSONResponse)
	if !is {
		t.Fatalf("response = %#v, want 400", resp)
	}
	if got.Code != api.VALIDATION || got.Args == nil {
		t.Fatalf("body = %+v, want VALIDATION with args", got.BadRequestJSONResponse)
	}
	if args := *got.Args; args["field"] != field || args["rule"] != rule {
		t.Errorf("args = %v, want field %s rule %s", args, field, rule)
	}
}

// ctxWithToken is ctx for a request that presented the session token, as
// RequestContextMiddleware would carry it.
func (h harness) ctxWithToken(t *testing.T, m member, ip, token string) context.Context {
	t.Helper()
	user, err := h.tdb.Queries.GetUserByID(context.Background(), m.id)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	return auth.WithUser(auth.ContextForTest(context.Background(), ip, "Go-Test", token), user)
}

// withBeginner is h with transactions begun through b, on the same database.
func (h harness) withBeginner(b auth.Beginner) harness {
	h.auth = auth.New(auth.Deps{
		Querier:            h.tdb.Queries,
		Beginner:           b,
		SessionTTL:         testTTL,
		SessionMaxLifetime: testMaxLifetime,
		CookieSecure:       true,
		FirstBackoff:       testFirstBackoff,
		MaxBackoff:         testMaxBackoff,
		Now:                h.now,
	})
	return h
}

// breakingQuerier fails exactly the calls a test names, as failingQuerier does,
// for the calls the User-settings operations make.
type breakingQuerier struct {
	db.Querier

	failGetCredential, failGetBackoff bool
}

func (b breakingQuerier) GetCredentialByUserID(ctx context.Context, id pgtype.UUID) (db.Credential, error) {
	if b.failGetCredential {
		return db.Credential{}, errOutage
	}
	return b.Querier.GetCredentialByUserID(ctx, id)
}

func (b breakingQuerier) GetLoginBackoff(ctx context.Context, keys []string) (float64, error) {
	if b.failGetBackoff {
		return 0, errOutage
	}
	return b.Querier.GetLoginBackoff(ctx, keys)
}

// failingTxBeginner begins real transactions that fail part-way: a statement
// whose SQL contains failOn, the Begin itself, or the Commit, as the test
// names. The earlier statements have already run, so the failure needs the
// rollback. The zero value fails nothing.
//
// A failed Commit rolls the real transaction back before it reports the
// failure, as a connection that dies during COMMIT leaves nothing behind, and
// so that the test's connection is not left holding the transaction open.
type failingTxBeginner struct {
	auth.Beginner
	failOn     string
	failBegin  bool
	failCommit bool
}

func (b failingTxBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	if b.failBegin {
		return nil, errOutage
	}
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failingTx{Tx: tx, failOn: b.failOn, failCommit: b.failCommit}, nil
}

type failingTx struct {
	pgx.Tx
	failOn     string
	failCommit bool
}

func (t failingTx) fails(sql string) bool {
	return t.failOn != "" && strings.Contains(sql, t.failOn)
}

func (t failingTx) Commit(ctx context.Context) error {
	if t.failCommit {
		_ = t.Rollback(ctx)
		return errOutage
	}
	return t.Tx.Commit(ctx)
}

func (t failingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if t.fails(sql) {
		return pgconn.CommandTag{}, errOutage
	}
	return t.Tx.Exec(ctx, sql, args...)
}

func (t failingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if t.fails(sql) {
		return nil, errOutage
	}
	return t.Tx.Query(ctx, sql, args...)
}

func (t failingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if t.fails(sql) {
		return failingRow{}
	}
	return t.Tx.QueryRow(ctx, sql, args...)
}

type failingRow struct{}

func (failingRow) Scan(...any) error { return errOutage }

// writeThenAnswerFailures are the four steps of a User-settings write that
// answers with the Household: the transaction's begin, the UPDATE, the read of
// the Household and the commit. Each is failed in turn, so a 500 from any of
// them must leave the users row as it was (the read and the commit with the
// UPDATE already run).
var writeThenAnswerFailures = map[string]failingTxBeginner{
	"begin":          {failBegin: true},
	"update":         {failOn: "UPDATE users"},
	"household read": {failOn: "FROM households"},
	"commit":         {failCommit: true},
}
