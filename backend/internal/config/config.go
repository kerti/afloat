// Package config is the only package that reads the environment
// (docs/adr/go/0003). Everything else takes what it needs as a parameter.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config is parsed once, in main, before anything else starts. A missing
// required value fails at boot rather than at the first request that needs it.
type Config struct {
	DatabaseURL string `env:"DATABASE_URL,required"`

	// 5182 per BOOTSTRAP.md §1: deliberately not 8080, which every other
	// project on the machine already wants, and below 32767 so it is never in
	// the ephemeral range.
	Port int `env:"PORT" envDefault:"5182"`

	// text for a terminal, json for anywhere logs are collected.
	LogFormat string `env:"LOG_FORMAT" envDefault:"text"`
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`

	// Applied by the binary on boot, mirroring Balances. Disable only to run a
	// server against a database migrated by something else — the Kotlin backend
	// against afloat_go, say, which is a debugging move and not a deployment.
	AutoMigrate bool `env:"AUTO_MIGRATE" envDefault:"true"`

	// ReadTimeout bounds a slow request body, WriteTimeout a slow reader, and
	// IdleTimeout an idle keep-alive connection. Self-hosting means there may be
	// no proxy in front, so these are the only defence against a client holding
	// a goroutine open indefinitely.
	ReadTimeout     time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"30s"`
	WriteTimeout    time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"60s"`
	IdleTimeout     time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"120s"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`

	// Which identity providers are live. Local is the only one built (PRD S2a);
	// Google is planned and additive (PRD §4.1), and the flag exists so enabling
	// it later is configuration rather than a new concept. GET /auth/methods
	// reports these so the client renders only what works.
	AuthLocalEnabled  bool `env:"AUTH_LOCAL_ENABLED" envDefault:"true"`
	AuthGoogleEnabled bool `env:"AUTH_GOOGLE_ENABLED" envDefault:"false"`

	// SessionTTL is the sliding window, refreshed once a session is past half
	// its life rather than on every request. SessionMaxLifetime is the absolute
	// cap measured from creation: without it a stolen cookie stays valid
	// indefinitely on continued use (BOOTSTRAP.md §5.1).
	SessionTTL         time.Duration `env:"SESSION_TTL" envDefault:"720h"`
	SessionMaxLifetime time.Duration `env:"SESSION_MAX_LIFETIME" envDefault:"2160h"`

	// The login backoff (BOOTSTRAP.md §5.1): LoginFirstBackoff after the first
	// failure, doubling per failure after it, capped at LoginMaxBackoff. The
	// curve these defaults produce is contract/testdata/login_backoff.json.
	LoginFirstBackoff time.Duration `env:"LOGIN_FIRST_BACKOFF" envDefault:"1s"`
	LoginMaxBackoff   time.Duration `env:"LOGIN_MAX_BACKOFF" envDefault:"5m"`

	// Off only for local development over plain HTTP. Every deployment leaves it
	// on — a session cookie without Secure travels in clear (BOOTSTRAP.md §5.1).
	CookieSecure bool `env:"COOKIE_SECURE" envDefault:"true"`

	// Version is stamped at build time via -ldflags and reported by GET /health.
	Version string `env:"VERSION" envDefault:"dev"`
}

// Load parses the environment and rejects a configuration that cannot serve.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse environment: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	// `env:"...,required"` accepts a variable that is SET BUT EMPTY, so
	// DATABASE_URL="" would pass parsing and fail at connect time instead — the
	// exact late failure this package exists to prevent.
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("DATABASE_URL is empty")
	}
	// A server nobody can sign in to is a misconfiguration, not a runtime
	// surprise to discover at the login screen.
	if !c.AuthLocalEnabled && !c.AuthGoogleEnabled {
		return fmt.Errorf("no identity provider enabled: set AUTH_LOCAL_ENABLED or AUTH_GOOGLE_ENABLED")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("PORT %d is not a valid port", c.Port)
	}
	switch c.LogFormat {
	case "text", "json":
	default:
		return fmt.Errorf("LOG_FORMAT %q: want text or json", c.LogFormat)
	}
	// A cap shorter than the window would expire every session early and make
	// the sliding refresh pointless.
	if c.SessionMaxLifetime < c.SessionTTL {
		return fmt.Errorf("SESSION_MAX_LIFETIME (%s) is shorter than SESSION_TTL (%s)",
			c.SessionMaxLifetime, c.SessionTTL)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LOG_LEVEL %q: want debug, info, warn or error", c.LogLevel)
	}
	// A zero or sub-millisecond first window is not a backoff a caller could
	// ever observe (#69): time.Duration can express nanoseconds, but nothing
	// in either backend's login path resolves that finely, so a value that
	// small is indistinguishable from no backoff at all. A cap below the
	// floor would also make the second failure's window shorter than the
	// first's.
	if c.LoginFirstBackoff < time.Millisecond {
		return fmt.Errorf("LOGIN_FIRST_BACKOFF (%s) must be at least 1ms", c.LoginFirstBackoff)
	}
	if c.LoginMaxBackoff < c.LoginFirstBackoff {
		return fmt.Errorf("LOGIN_MAX_BACKOFF (%s) is shorter than LOGIN_FIRST_BACKOFF (%s)",
			c.LoginMaxBackoff, c.LoginFirstBackoff)
	}
	// http.Server reads 0 as "no timeout", but the same value also feeds
	// middleware.Timeout (#30), where 0 is a deadline already passed: every
	// request would be cancelled on arrival. Refused at boot, in both backends.
	if c.WriteTimeout <= 0 {
		return fmt.Errorf("HTTP_WRITE_TIMEOUT (%s) must be positive", c.WriteTimeout)
	}
	return nil
}
