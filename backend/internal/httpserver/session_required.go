package httpserver

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/go-chi/chi/v5"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/auth"
	"github.com/kerti/afloat/backend/internal/httperr"
)

// The contract's securitySchemes names. Any other name in a `security`
// requirement is refused at startup (sessionRequirements): the enforcement
// below only understands these two.
const (
	sessionCookieScheme    = "sessionCookie"
	onboardingCookieScheme = "onboardingCookie"
)

// requiresSession reports whether an operation's effective security demands a
// session: there is a requirement list, no alternative in it is empty (an
// empty `{}` is the contract's way of saying "no credential is fine"), and one
// alternative names the session cookie.
//
// An operation that asks only for another scheme, such as onboardingCookie,
// is not this middleware's to judge: that credential is looked up in the
// handler, which owns its 401 shape and its cookie clearing.
func requiresSession(security openapi3.SecurityRequirements) bool {
	if len(security) == 0 {
		return false
	}
	named := false
	for _, alternative := range security {
		if len(alternative) == 0 {
			return false
		}
		if _, ok := alternative[sessionCookieScheme]; ok {
			named = true
		}
	}
	return named
}

// effectiveSecurity is the requirement list an operation answers to: its own
// when it declares one (an empty list opts out), else the contract's global
// one.
func effectiveSecurity(spec *openapi3.T, op *openapi3.Operation) openapi3.SecurityRequirements {
	if op.Security != nil {
		return *op.Security
	}
	return spec.Security
}

// sessionRequirements maps "METHOD /contract/path" to whether the operation
// requires a session, for every operation the contract declares.
//
// A scheme name it does not know is an error rather than a pass: a typo such
// as `sessionCookies` would otherwise match nothing and quietly make the
// operation public. So is an operation naming both the session and the
// onboarding cookie.
func sessionRequirements(spec *openapi3.T) (map[string]bool, error) {
	required := map[string]bool{}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			security := effectiveSecurity(spec, op)
			namesSession, namesOnboarding := false, false
			for _, alternative := range security {
				for name := range alternative {
					switch name {
					case sessionCookieScheme:
						namesSession = true
					case onboardingCookieScheme:
						namesOnboarding = true
					default:
						return nil, fmt.Errorf("%s %s: unknown security scheme %q", method, path, name)
					}
				}
			}
			// sessionRequired can check a session but not a handshake, which
			// is a database lookup with its own 401 in the onboarding handlers.
			// An operation naming both has no rule yet; refusing it at startup
			// forces that decision on the slice that first needs it, rather
			// than a runtime 401 for a valid handshake holder (#114).
			if namesSession && namesOnboarding {
				return nil, fmt.Errorf("%s %s: names both %s and %s; the session middleware cannot check a handshake, so this needs a ruling first", method, path, sessionCookieScheme, onboardingCookieScheme)
			}
			required[method+" "+path] = requiresSession(security)
		}
	}
	return required, nil
}

// contractSessionRequirements is sessionRequirements over the embedded
// contract, which the server cannot start without.
func contractSessionRequirements() map[string]bool {
	spec, err := api.GetSpec()
	if err != nil {
		panic("httpserver: load embedded openapi spec: " + err.Error())
	}
	required, err := sessionRequirements(spec)
	if err != nil {
		panic("httpserver: " + err.Error())
	}
	return required
}

// operationMiddlewaresFor is the per-operation chain mountAPI hands the
// generated router.
//
// The generated wrapper applies the list in order, each entry wrapping the
// last, so the LAST entry runs FIRST. sessionRequired is last on purpose: an
// unauthenticated caller is refused before the validator can tell them
// anything about their body (#114). Path parameters are bound before any of
// this runs, which is why paramErrorHandlerFor asks the same question.
func operationMiddlewaresFor(required map[string]bool) []api.MiddlewareFunc {
	return []api.MiddlewareFunc{openapiRequestValidator(), sessionRequired(required)}
}

// sessionRequired is where a session is enforced, as per-operation middleware
// (refusedForSession is the decision, which paramErrorHandlerFor asks too, for
// the one thing that runs ahead of this: path parameter binding). It reads the
// matched operation's effective `security` from the contract and answers 401
// UNAUTHORIZED when that requires a session and SessionMiddleware attached no
// User (500 INTERNAL instead when it could not read the session at all, an
// outage), so a new operation is protected by default and a handler that wants a
// User only has to ask for it (#114).
//
// It is per-operation middleware (ChiServerOptions.Middlewares in mountAPI),
// so it only ever runs once chi has matched a route the contract declares:
// an unknown path or method keeps chi's 404 or 405. The operation is
// identified from chi's own match, RoutePattern, so there is no second router
// to disagree with the first.
//
// It sits outside openapiRequestValidator (operationMiddlewares), so a caller
// without a session learns nothing about what the validator would have said of
// their body. required is sessionRequirements' map of the contract.
func sessionRequired(required map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if refusedForSession(w, r, required) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// refusedForSession answers the request itself, and reports true, when the
// matched operation needs a session the request does not have: 401, or 500 for
// a session that could not be read. It is the one place that decision is made,
// shared by sessionRequired and by paramErrorHandler (see there for why).
//
// chi's pattern is the mount point plus the generated route, and its {name}
// parameters are OpenAPI's own syntax. headAsGet has already rewritten a HEAD
// to the GET it is answered by.
func refusedForSession(w http.ResponseWriter, r *http.Request, required map[string]bool) bool {
	pattern := strings.TrimPrefix(chi.RouteContext(r.Context()).RoutePattern(), basePath)
	needsSession, known := required[r.Method+" "+pattern]
	if !known {
		// Every generated route is a contract operation, so this is our
		// fault, not the client's - and "allow" would make a mismatch
		// silently unprotected.
		slog.Error("session check: no contract operation for route", "method", r.Method, "pattern", pattern)
		httperr.Write(w, http.StatusInternalServerError, httperr.CodeInternal, nil)
		return true
	}
	if !needsSession {
		return false
	}
	if _, ok := auth.UserFromContext(r.Context()); ok {
		return false
	}
	// A session that could not be read is not one that is invalid: 401 would
	// tell the client to sign in again over an outage. The cookie was left
	// alone (#27).
	if auth.SessionUnresolved(r.Context()) {
		httperr.Write(w, http.StatusInternalServerError, httperr.CodeInternal, nil)
		return true
	}
	httperr.Write(w, http.StatusUnauthorized, httperr.CodeUnauthorized, nil)
	return true
}
