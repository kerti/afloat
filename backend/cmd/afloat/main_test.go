package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/config"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/httpserver"
	"github.com/kerti/afloat/backend/internal/system"
)

// slowQuerier's Ping blocks until its context is cancelled, standing in for a
// handler that overruns HTTP_WRITE_TIMEOUT.
type slowQuerier struct{ db.Querier }

func (slowQuerier) Ping(ctx context.Context) (int32, error) {
	<-ctx.Done()
	return 0, ctx.Err()
}

// A handler cut off by HTTP_WRITE_TIMEOUT must still answer. With the write
// deadline equal to the handler timeout, the deadline passed first and the
// client saw EOF instead of the 503 (#30); httptest.NewRecorder has no write
// deadline, so only a real server and connection can catch that.
func TestHandlerTimeoutAnswerReachesClient(t *testing.T) {
	cfg := config.Config{WriteTimeout: 200 * time.Millisecond}
	q := slowQuerier{}
	srv := newHTTPServer(cfg, httpserver.New(httpserver.Deps{
		System:         system.New(system.Deps{Querier: q, Version: "test", LocalEnabled: true}),
		Auth:           auth.New(auth.Deps{Querier: q}),
		HandlerTimeout: cfg.WriteTimeout,
	}))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	resp, err := http.Get("http://" + ln.Addr().String() + "/api/health")
	if err != nil {
		t.Fatalf("client got %v, want the handler's 503: the write deadline passed before the handler timeout", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d (%s), want %d", resp.StatusCode, body, http.StatusServiceUnavailable)
	}
}

// #66: net/http answers a bare "OPTIONS *" itself, before Handler ever runs,
// unless DisableGeneralOptionsHandler is set - httptest.NewRecorder cannot
// catch this either, since that shortcut lives in Server.Serve, not in
// Handler. Without it, this answered 200 with an empty body: no Allow header,
// but also never optionsRefused's flat 405 (httpserver/middleware.go), so
// this one path stayed distinguishable from every other OPTIONS.
func TestOptionsStarReachesTheHandler(t *testing.T) {
	q := db.Querier(nil)
	cfg := config.Config{WriteTimeout: time.Second}
	srv := newHTTPServer(cfg, httpserver.New(httpserver.Deps{
		System:         system.New(system.Deps{Querier: q, Version: "test", LocalEnabled: true}),
		Auth:           auth.New(auth.Deps{Querier: q}),
		HandlerTimeout: cfg.WriteTimeout,
	}))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	// A server bug here is "answers nothing", not "answers wrong" - hanging
	// forever with a passing test suite is worse than a clear timeout.
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("OPTIONS * HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	answer := string(raw)
	statusLine, _, _ := strings.Cut(answer, "\r\n")
	if statusLine != "HTTP/1.1 405 Method Not Allowed" {
		t.Errorf("OPTIONS * status line = %q, want 405 (optionsRefused, not net/http's built-in 200)", statusLine)
	}
	if strings.Contains(strings.ToLower(answer), "allow:") {
		t.Errorf("OPTIONS * answer carries an Allow header, which optionsRefused never sends: %q", answer)
	}
}

// #92: the header section is capped at maxHeaderBytes, not net/http's 1 MiB
// default. Only a real server can show it - the cap is conn.readRequest's read
// limit, which httptest.NewRecorder never touches. Fixed sizes, not ones
// derived from the constant: 7 KiB must pass, and 16 KiB - past the cap plus
// net/http's 4 KiB slop - must be refused, so raising the cap past about 12 KiB
// fails here.
func TestOversizeHeadersAreRefused(t *testing.T) {
	q := db.Querier(nil)
	cfg := config.Config{WriteTimeout: time.Second}
	srv := newHTTPServer(cfg, httpserver.New(httpserver.Deps{
		System:         system.New(system.Deps{Querier: q, Version: "test", LocalEnabled: true}),
		Auth:           auth.New(auth.Deps{Querier: q}),
		HandlerTimeout: cfg.WriteTimeout,
	}))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	statusFor := func(headerBytes int) string {
		t.Helper()
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		req := "GET /api/no-such-route HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n" +
			"X-Padding: " + strings.Repeat("a", headerBytes) + "\r\n\r\n"
		// The write may fail part-way: a server that has already answered 431
		// closes the connection. The answer is still there to read.
		_, _ = conn.Write([]byte(req))
		raw, _ := io.ReadAll(conn)
		statusLine, _, _ := strings.Cut(string(raw), "\r\n")
		return statusLine
	}

	if got := statusFor(7 << 10); !strings.Contains(got, "404") {
		t.Errorf("a 7 KiB header section answered %q, want the route's 404", got)
	}
	if got := statusFor(16 << 10); got != "HTTP/1.1 431 Request Header Fields Too Large" {
		t.Errorf("a 16 KiB header section answered %q, want 431 - is MaxHeaderBytes still 8 KiB?", got)
	}
}
