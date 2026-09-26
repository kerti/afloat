package config

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	// Genuinely unset, not set-to-empty: the two are different to env parsing,
	// and a variable left in the developer's shell must not change a result.
	for _, k := range []string{
		"DATABASE_URL", "PORT", "LOG_FORMAT", "LOG_LEVEL", "AUTO_MIGRATE",
		"AUTH_LOCAL_ENABLED", "AUTH_GOOGLE_ENABLED", "COOKIE_SECURE", "VERSION",
		"HTTP_WRITE_TIMEOUT", "LOGIN_FIRST_BACKOFF", "LOGIN_MAX_BACKOFF",
	} {
		if old, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { _ = os.Setenv(k, old) })
		}
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("unset %s: %v", k, err)
		}
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x/y"})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// 5182 is BOOTSTRAP.md §1's allocation. If this ever silently becomes 8080
	// the whole point of that section is lost.
	if cfg.Port != 5182 {
		t.Errorf("Port = %d, want 5182", cfg.Port)
	}
	if !cfg.AuthLocalEnabled {
		t.Error("AuthLocalEnabled = false, want true: local is the only MVP provider")
	}
	if cfg.AuthGoogleEnabled {
		t.Error("AuthGoogleEnabled = true, want false: Google is planned, not built")
	}
	if !cfg.CookieSecure {
		t.Error("CookieSecure = false, want true: insecure must be opt-in, never the default")
	}
	if !cfg.AutoMigrate {
		t.Error("AutoMigrate = false, want true")
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		setEnv(t, nil)
		if _, err := Load(); err == nil {
			t.Fatal("Load succeeded with no DATABASE_URL; it must fail at boot")
		}
	})

	// Set-but-empty is a separate case: env's `required` accepts it, so without
	// an explicit check the failure moves to connect time.
	t.Run("empty", func(t *testing.T) {
		setEnv(t, map[string]string{"DATABASE_URL": ""})
		if _, err := Load(); err == nil {
			t.Fatal("Load accepted an empty DATABASE_URL")
		}
	})
}

func TestLoadRejectsNoIdentityProvider(t *testing.T) {
	setEnv(t, map[string]string{
		"DATABASE_URL":        "postgres://x/y",
		"AUTH_LOCAL_ENABLED":  "false",
		"AUTH_GOOGLE_ENABLED": "false",
	})

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded with every provider disabled; nobody could sign in")
	}
	if !strings.Contains(err.Error(), "identity provider") {
		t.Errorf("error = %q, want it to name the identity provider problem", err)
	}
}

func TestLoadRejectsBadEnums(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"log format", "LOG_FORMAT", "xml"},
		{"log level", "LOG_LEVEL", "verbose"},
		{"port", "PORT", "70000"},
		// 0 would cancel every request on arrival (#30), not disable the bound.
		{"zero write timeout", "HTTP_WRITE_TIMEOUT", "0s"},
		{"negative write timeout", "HTTP_WRITE_TIMEOUT", "-1s"},
		// No backoff at all is not a backoff, and below 1ms is not one a
		// caller could ever observe either (#69).
		{"zero first backoff", "LOGIN_FIRST_BACKOFF", "0s"},
		{"negative first backoff", "LOGIN_FIRST_BACKOFF", "-1s"},
		{"first backoff below 1ms", "LOGIN_FIRST_BACKOFF", "999us"},
		// S3: whitespace-only is not the same as unset (#69's empty-string
		// rule) - caarlos0/env's getOr only special-cases `value == ""`, so
		// this reaches time.ParseDuration and is refused like any other
		// unparseable spelling.
		{"first backoff is whitespace", "LOGIN_FIRST_BACKOFF", "   "},
		{"first backoff is a tab", "LOGIN_FIRST_BACKOFF", "\t"},
		// A cap below the first window: the second failure would wait less
		// than the first.
		{"max backoff below the first", "LOGIN_MAX_BACKOFF", "500ms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, map[string]string{"DATABASE_URL": "postgres://x/y", tc.key: tc.value})
			if _, err := Load(); err == nil {
				t.Errorf("Load accepted %s=%s", tc.key, tc.value)
			}
		})
	}
}

// The backoff defaults are the parameters contract/testdata/login_backoff.json
// computes its curve from (#16). Kotlin's AppConfigSpec holds its defaults to
// the same file, so a default changed in one backend fails there.
func TestBackoffDefaultsMatchTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../contract/testdata/login_backoff.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fx struct {
		Parameters struct {
			FirstBackoff string `json:"first_backoff"`
			MaxBackoff   string `json:"max_backoff"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	first, err := time.ParseDuration(fx.Parameters.FirstBackoff)
	if err != nil {
		t.Fatalf("fixture first_backoff: %v", err)
	}
	maxBackoff, err := time.ParseDuration(fx.Parameters.MaxBackoff)
	if err != nil {
		t.Fatalf("fixture max_backoff: %v", err)
	}

	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x/y"})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LoginFirstBackoff != first || cfg.LoginMaxBackoff != maxBackoff {
		t.Errorf("defaults are %s and %s, the fixture's are %s and %s",
			cfg.LoginFirstBackoff, cfg.LoginMaxBackoff, first, maxBackoff)
	}
}

// #69: 1ms is the floor, not a value just under it.
func TestLoadAcceptsLoginFirstBackoffAtTheFloor(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x/y", "LOGIN_FIRST_BACKOFF": "1ms"})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LoginFirstBackoff != time.Millisecond {
		t.Errorf("LoginFirstBackoff = %s, want 1ms", cfg.LoginFirstBackoff)
	}
}

// #69: a duration variable set to the empty string is unset, not a parse
// failure - "FOO=" in a .env boots with FOO's documented default. Every
// duration variable, not only the login backoffs this issue started from.
func TestBlankDurationVarsFallBackToTheirDefault(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want time.Duration
	}{
		{"HTTP_READ_TIMEOUT", 30 * time.Second},
		{"HTTP_WRITE_TIMEOUT", 60 * time.Second},
		{"HTTP_IDLE_TIMEOUT", 120 * time.Second},
		{"SHUTDOWN_TIMEOUT", 10 * time.Second},
		{"SESSION_TTL", 720 * time.Hour},
		{"SESSION_MAX_LIFETIME", 2160 * time.Hour},
		{"LOGIN_FIRST_BACKOFF", time.Second},
		{"LOGIN_MAX_BACKOFF", 5 * time.Minute},
	} {
		t.Run(tc.key, func(t *testing.T) {
			setEnv(t, map[string]string{"DATABASE_URL": "postgres://x/y", tc.key: ""})
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			got := durationFieldByEnvName(cfg, tc.key)
			if got != tc.want {
				t.Errorf("%s=\"\": got %s, want the default %s", tc.key, got, tc.want)
			}
		})
	}
}

// durationFieldByEnvName reads the table above against the struct Load
// itself parses, rather than a second, driftable copy of the field list.
func durationFieldByEnvName(cfg Config, envName string) time.Duration {
	v := reflect.ValueOf(cfg)
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("env"), ",")
		if name == envName {
			return v.Field(i).Interface().(time.Duration)
		}
	}
	panic("no Config field tagged env:" + envName)
}
