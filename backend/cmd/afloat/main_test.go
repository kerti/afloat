package main

import (
	"context"
	"io"
	"net"
	"net/http"
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
