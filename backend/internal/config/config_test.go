package config

import (
	"os"
	"strings"
	"testing"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	// Genuinely unset, not set-to-empty: the two are different to env parsing,
	// and a variable left in the developer's shell must not change a result.
	for _, k := range []string{
		"DATABASE_URL", "PORT", "LOG_FORMAT", "LOG_LEVEL", "AUTO_MIGRATE",
		"AUTH_LOCAL_ENABLED", "AUTH_GOOGLE_ENABLED", "COOKIE_SECURE", "VERSION",
		"HTTP_WRITE_TIMEOUT",
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, map[string]string{"DATABASE_URL": "postgres://x/y", tc.key: tc.value})
			if _, err := Load(); err == nil {
				t.Errorf("Load accepted %s=%s", tc.key, tc.value)
			}
		})
	}
}
