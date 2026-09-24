package auth

import "context"

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

// HoldArgonPermitsForTest takes every permit, so the next Argon2 call queues
// until its context ends, and returns the func that gives them back.
func HoldArgonPermitsForTest() (release func()) {
	for range argonConcurrencyCap {
		argonSem <- struct{}{}
	}
	return func() {
		for range argonConcurrencyCap {
			<-argonSem
		}
	}
}
