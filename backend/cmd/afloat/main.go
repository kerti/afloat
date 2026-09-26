// Command afloat is the Go backend: the canonical implementation of
// contract/openapi.yaml (BOOTSTRAP.md §2).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/config"
	"github.com/kerti/afloat/backend/internal/db"
	"github.com/kerti/afloat/backend/internal/httpserver"
	"github.com/kerti/afloat/backend/internal/postgres"
	"github.com/kerti/afloat/backend/internal/system"
)

// Short and fixed: no legitimate client needs longer to send its headers, and
// leaving it at Go's default of "however long ReadTimeout is" keeps a goroutine
// per stalled connection.
const readHeaderTimeout = 5 * time.Second

// writeTimeoutGrace is how much longer the connection's write deadline runs
// than the handler timeout. Both come from HTTP_WRITE_TIMEOUT (#30), but they
// cannot be equal: http.Server starts its deadline when the headers are read,
// before middleware.Timeout starts its own, so the write deadline would always
// pass first and the handler's 503 would be dropped with the connection. The
// grace is the time that answer has to reach the client.
const writeTimeoutGrace = 5 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		// Logging is not configured yet, so this one goes to stderr directly.
		return err
	}
	slog.SetDefault(newLogger(cfg))

	// Cancelled on SIGINT/SIGTERM, which starts graceful shutdown below.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.AutoMigrate {
		if err := postgres.Migrate(ctx, cfg.DatabaseURL); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	} else {
		slog.Warn("AUTO_MIGRATE is off; assuming the schema is already current")
	}

	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	queries := db.New(pool)

	handler := httpserver.New(httpserver.Deps{
		Auth: auth.New(auth.Deps{
			Querier:            queries,
			Beginner:           pool,
			SessionTTL:         cfg.SessionTTL,
			SessionMaxLifetime: cfg.SessionMaxLifetime,
			CookieSecure:       cfg.CookieSecure,
			FirstBackoff:       cfg.LoginFirstBackoff,
			MaxBackoff:         cfg.LoginMaxBackoff,
		}),
		System: system.New(system.Deps{
			Querier:       queries,
			Version:       cfg.Version,
			LocalEnabled:  cfg.AuthLocalEnabled,
			GoogleEnabled: cfg.AuthGoogleEnabled,
		}),
		// The same variable newHTTPServer derives the write deadline from: one
		// bound on how long a handler may run, not a second literal that could
		// drift from it (#30).
		HandlerTimeout: cfg.WriteTimeout,
	})

	srv := newHTTPServer(cfg, handler)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "port", cfg.Port, "version", cfg.Version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	// Let in-flight requests finish rather than cutting them off mid-write.
	slog.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	slog.Info("stopped")
	return nil
}

func newHTTPServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:    net.JoinHostPort("", strconv.Itoa(cfg.Port)),
		Handler: handler,
		// Separate from ReadTimeout: this one bounds a client that opens a
		// connection and dribbles headers, which ReadTimeout does not cover.
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout + writeTimeoutGrace,
		IdleTimeout:       cfg.IdleTimeout,
		// Without this, net/http answers OPTIONS * itself - a bare 200, never
		// reaching Handler - before optionsRefused (httpserver/middleware.go)
		// gets a chance to answer it the same flat 405 as every other OPTIONS
		// (#66). Tomcat's CoyoteAdapter answers OPTIONS * the same way net/http
		// would have, before any of Kotlin's own filters run, with no
		// equivalent switch to disable it - a permitted difference instead
		// (permitted-differences.yaml).
		DisableGeneralOptionsHandler: true,
	}
}

func newLogger(cfg config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
