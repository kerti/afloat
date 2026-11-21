package auth

import (
	"context"
	"sync"
)

// Hooks for the auth_test integration tests. A _test.go file, so they are
// compiled into this package's test binary and never into the server.

// ContextForTest builds the request-scoped context a handler expects, without a
// real request.
func ContextForTest(ctx context.Context, ip, userAgent, sessionToken string) context.Context {
	ctx = context.WithValue(ctx, clientIPKey{}, ip)
	ctx = context.WithValue(ctx, userAgentKey{}, userAgent)
	if sessionToken != "" {
		ctx = context.WithValue(ctx, sessionTokenKey{}, sessionToken)
	}
	return ctx
}

// WithOnboardingTokenForTest adds the handshake cookie's value to a context
// built by ContextForTest, as RequestContextMiddleware would.
func WithOnboardingTokenForTest(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, onboardingTokenKey{}, token)
}

// VerifyForTest checks a password against a stored PHC string, taking the
// Argon2 permit verifyHoldingPermit requires.
func VerifyForTest(password, phc string) bool {
	if err := acquireArgonPermit(context.Background()); err != nil {
		return false
	}
	defer releaseArgonPermit()
	return verifyHoldingPermit(password, phc)
}

// ArgonConcurrencyCapForTest reports the fixed cap.
func ArgonConcurrencyCapForTest() int32 {
	return argonConcurrencyCap
}

// ArgonPeakInFlightForTest reports the most Argon2 calls that have held a
// permit at once since the last reset.
func ArgonPeakInFlightForTest() int32 {
	return argonPeakInFlight.Load()
}

// ResetArgonPeakInFlightForTest zeroes ArgonPeakInFlightForTest.
func ResetArgonPeakInFlightForTest() {
	argonPeakInFlight.Store(0)
}

// ArgonWaitingForTest reports how many callers are queued for a permit.
func ArgonWaitingForTest() int32 {
	return argonWaiting.Load()
}

// HoldArgonPermitsForTest takes every permit, so the next Argon2 call queues
// until its context ends, and returns the func that gives them back. That
// func is safe to call more than once, so a test can release mid-way and still
// defer it.
func HoldArgonPermitsForTest() (release func()) {
	for range argonConcurrencyCap {
		argonSem <- struct{}{}
	}
	return sync.OnceFunc(func() {
		for range argonConcurrencyCap {
			<-argonSem
		}
	})
}

// TakeArgonPermitsForTest takes n permits, so that fewer than the cap are left
// for callers, and GiveBackArgonPermitsForTest returns them. Unlike
// HoldArgonPermitsForTest it can hand back a few at a time, which is what lets a
// test have exactly one caller run while the rest wait queued.
func TakeArgonPermitsForTest(n int) {
	for range n {
		argonSem <- struct{}{}
	}
}

// GiveBackArgonPermitsForTest returns n permits taken by TakeArgonPermitsForTest.
func GiveBackArgonPermitsForTest(n int) {
	for range n {
		<-argonSem
	}
}
