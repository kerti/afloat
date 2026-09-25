// Package auth owns local email + password authentication and server-side
// sessions (BOOTSTRAP.md §5).
package auth

import (
	"time"

	"github.com/kerti/afloat/backend/internal/db"
)

// Handlers implements the authentication half of api.StrictServerInterface.
type Handlers struct {
	q  db.Querier
	tx Beginner

	sessionTTL         time.Duration
	sessionMaxLifetime time.Duration
	cookieSecure       bool

	// The backoff for failed logins: firstBackoff after the first, doubling per
	// failure after it, capped at maxBackoff. Backoff, never a hard lockout - a
	// lockout on a self-hosted household app locks the household out of its own
	// data. LOGIN_FIRST_BACKOFF and LOGIN_MAX_BACKOFF (BOOTSTRAP.md §12).
	firstBackoff time.Duration
	maxBackoff   time.Duration

	// now is a seam for tests, which need to reach a session's expiry without
	// waiting thirty days for it.
	now func() time.Time
}

type Deps struct {
	Querier  db.Querier
	Beginner Beginner

	// SessionTTL is the sliding window, refreshed once a session is past half
	// its life. SessionMaxLifetime is the absolute cap measured from creation:
	// without it, a stolen cookie stays valid indefinitely on continued use,
	// which is the gap in Balances' implementation (BOOTSTRAP.md §5.1).
	SessionTTL         time.Duration
	SessionMaxLifetime time.Duration
	CookieSecure       bool
	FirstBackoff       time.Duration
	MaxBackoff         time.Duration
	Now                func() time.Time
}

func New(d Deps) *Handlers {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Handlers{
		q:                  d.Querier,
		tx:                 d.Beginner,
		sessionTTL:         d.SessionTTL,
		sessionMaxLifetime: d.SessionMaxLifetime,
		cookieSecure:       d.CookieSecure,
		firstBackoff:       d.FirstBackoff,
		maxBackoff:         d.MaxBackoff,
		now:                now,
	}
}
