package httpserver

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/httperr"
)

// The spec-validating middleware (#18, #21): every request that reaches a
// defined operation is checked against the embedded contract/openapi.yaml
// before its handler runs, so a constraint added to the contract (required,
// maxLength, additionalProperties: false, ...) is enforced everywhere rather
// than hand-copied per handler. It is mounted per-operation via
// ChiServerOptions.Middlewares in New (server.go), not as a top-level r.Use:
// that means it only ever sees a request chi has already matched to a defined
// route, so an undefined path or method is unaffected and keeps chi's own
// 404/405 handling exactly as before.

// registerEmailFormat teaches kin-openapi what `format: email` means.
// kin-openapi ships no built-in "email" format validator (schema_formats.go
// registers byte, date, date-time, ipv4, ipv6 by default; email is not among
// them), so an unregistered `format: email` silently accepts anything. This
// defers to httperr.Validator()'s own "email" tag rather than a second regex,
// so the contract's notion of a valid address and the struct-tag vocabulary
// the rest of the envelope uses agree by construction.
//
// It trims before validating: the ruling on #18 is "trim, then validate" for
// a padded address (#14 test 17, emailIsMatchedCaseInsensitivelyAndTrimmed) —
// this format check is the only place in the request path that runs before a
// handler gets a chance to normalise anything, so the trim has to happen
// here or the padded form 400s before login.go's normalizeEmail ever runs.
// normalizeEmail still trims again for the lookup; this only decides whether
// the padded form is accepted at all. The trim is local to this check, so
// maxLength still counts the padding: only an address within a few spaces of
// the 320 cap, far past RFC 5321's 254, answers differently for it.
//
// DefineStringFormatValidator is process-global state in kin-openapi, so this
// runs once via sync.OnceFunc rather than on every middleware build.
var registerEmailFormat = sync.OnceFunc(func() {
	openapi3.DefineStringFormatValidator("email", openapi3.NewCallbackValidator(func(v string) error {
		if err := httperr.Validator().Var(strings.TrimSpace(v), "email"); err != nil {
			return errors.New("not a valid email address")
		}
		return nil
	}))
})

// openapiRequestValidator builds the middleware from the embedded spec
// (api.GetSpec, generated via oapi-codegen.yaml's embedded-spec: true —
// never hand-patch api.gen.go to add this; the generator config already
// owns it).
//
// AuthenticationFunc is a no-op: the contract's `security: [sessionCookie]`
// documents that most routes expect a session, but enforcing that is
// deliberately NOT this middleware's job. server.go's SessionMiddleware
// resolves a session into a User but never rejects — each handler decides
// for itself whether it needs one, so that a new authenticated route can't be
// added without a deliberate choice. Wiring a real AuthenticationFunc here
// would make the OpenAPI layer a second, earlier enforcement point with its
// own opinion, which disagrees with that design the moment the two diverge.
//
// MultiError is on so that every failure is collected and the reported one
// is chosen the way Kotlin chooses it (writeOpenAPIValidationError), not
// whichever check kin-openapi happens to run first.
func openapiRequestValidator() func(http.Handler) http.Handler {
	registerEmailFormat()

	spec, err := api.GetSpec()
	if err != nil {
		// Only reachable if the embedded spec itself is corrupt, which means
		// oapi-codegen's own generation was broken — every other generated
		// type would be suspect too. Nothing about this process is trustworthy.
		panic("httpserver: load embedded openapi spec: " + err.Error())
	}

	return nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			MultiError:         true,
		},
		// The servers entry is a bare path ("/api"), not a host, so kin-openapi's
		// router matches no Host header: silencing this skips the Host-header
		// warning nethttp-middleware prints for any spec that declares a server.
		SilenceServersWarning: true,
		ErrorHandlerWithOpts:  writeOpenAPIValidationError,
	})
}

// writeOpenAPIValidationError translates the middleware's error into the
// shared envelope, answering exactly as Kotlin's ApiExceptionHandler does for
// the same body:
//
//   - A body that is not an instance of the declared shape at all — malformed
//     JSON, a wrong type, a null, a property the contract does not declare —
//     is INVALID_JSON_BODY, whatever else is wrong with it. Kotlin's Jackson
//     decode fails on all of these before Bean Validation ever runs, and Go's
//     own generated decode answers the same code (requestErrorHandler).
//   - Otherwise every failure is a named field breaking a constraint, and the
//     one reported is the least by (field, rule): Kotlin's rule (#13 §3.5),
//     which kin-openapi's own order — present properties, sorted, then
//     required in declared order — does not match once two fields fail.
//
// It writes through httperr.Write rather than httperr.WriteValidation: that
// helper is typed to validator.ValidationErrors, and kin-openapi's errors are
// a structurally different type. Same envelope, different plumbing.
//
// Nothing here logs a kin-openapi error's own text for a schema or parameter
// failure: SchemaError.Error() prints the offending value, and on the login
// route that value can be the password.
func writeOpenAPIValidationError(_ context.Context, err error, w http.ResponseWriter, r *http.Request, _ nethttpmiddleware.ErrorHandlerOpts) {
	var secErr *openapi3filter.SecurityRequirementsError
	if errors.As(err, &secErr) {
		// Unreachable while AuthenticationFunc above is the no-op: it never
		// fails a security requirement. Kept so a future change to that
		// function degrades to the right code instead of a 500 below.
		slog.Warn("openapi validator: unexpected security failure", "path", r.URL.Path)
		httperr.Write(w, http.StatusUnauthorized, httperr.CodeUnauthorized, nil)
		return
	}

	var found []failure
	if fault := collectFailures(err, &found); fault != nil || len(found) == 0 {
		// Not a verdict on the request: kin-openapi's router could not find the
		// operation chi had already matched, or validation itself broke. Either
		// way the fault is ours, and blaming the client's body would hide it.
		slog.Error("openapi validator: request not validated", "path", r.URL.Path, "err", fault)
		httperr.Write(w, http.StatusInternalServerError, httperr.CodeInternal, nil)
		return
	}

	for _, f := range found {
		if f.undecodable != "" {
			slog.Warn("request body rejected by contract", "path", r.URL.Path, "reason", f.undecodable)
			httperr.Write(w, http.StatusBadRequest, httperr.CodeInvalidJSONBody, nil)
			return
		}
	}

	first := slices.MinFunc(found, func(a, b failure) int {
		return cmp.Or(cmp.Compare(a.field, b.field), cmp.Compare(a.rule, b.rule))
	})
	httperr.Write(w, http.StatusBadRequest, httperr.CodeValidation, map[string]any{
		"field": first.field,
		"rule":  first.rule,
	})
}

// failure is one leaf of kin-openapi's error tree: either a named field
// breaking a constraint, or a request that did not decode as the contract
// declares, described safely enough to log.
type failure struct {
	field, rule string
	undecodable string
}

// collectFailures flattens err into found, returning the first leaf that is
// not a verdict on the request at all.
func collectFailures(err error, found *[]failure) (fault error) {
	switch e := err.(type) {
	case openapi3.MultiError:
		for _, inner := range e {
			if fault := collectFailures(inner, found); fault != nil {
				return fault
			}
		}
		return nil
	case *openapi3filter.RequestError:
		if e.Parameter != nil {
			*found = append(*found, failure{field: e.Parameter.Name, rule: parameterRule(e.Err)})
			return nil
		}
		if e.RequestBody != nil && e.Err != nil {
			collectBodyFailures(e.Err, found)
			return nil
		}
		// A required body that is empty, or a Content-Type the operation does
		// not list. kin-openapi's text for these carries no body content.
		*found = append(*found, failure{undecodable: e.Error()})
		return nil
	default:
		return err
	}
}

func collectBodyFailures(err error, found *[]failure) {
	switch e := err.(type) {
	case openapi3.MultiError:
		for _, inner := range e {
			collectBodyFailures(inner, found)
		}
	case *openapi3.SchemaError:
		pointer := e.JSONPointer()
		if rule := constraintRule(e); rule != "" && len(pointer) > 0 {
			*found = append(*found, failure{field: strings.Join(pointer, "."), rule: rule})
			return
		}
		*found = append(*found, failure{
			undecodable: fmt.Sprintf("%s at /%s", e.SchemaField, strings.Join(pointer, "/")),
		})
	default:
		// The body did not parse (a *ParseError, whose JSON decoder text names
		// a character or offset, never the body), or could not be read at all,
		// e.g. cut short by maxBodyBytes.
		*found = append(*found, failure{undecodable: err.Error()})
	}
}

// parameterRule mirrors paramFieldAndRule (errors.go): a parameter that binds
// but breaks the contract gets the same vocabulary as one that fails binding.
func parameterRule(err error) string {
	if errors.Is(err, openapi3filter.ErrInvalidRequired) {
		return "required"
	}
	var schemaErr *openapi3.SchemaError
	if errors.As(err, &schemaErr) {
		if rule := constraintRule(schemaErr); rule != "" {
			return rule
		}
	}
	return "invalid"
}

// constraintRule maps a kin-openapi schema keyword onto the {rule} vocabulary
// httperr.WriteValidation and Kotlin's ruleOf already share —
// go-playground/validator's tag names — or "" for a keyword Kotlin enforces
// by failing the decode instead (type, nullable, additionalProperties, enum,
// ...), which is INVALID_JSON_BODY, not VALIDATION.
func constraintRule(err *openapi3.SchemaError) string {
	switch err.SchemaField {
	case "required":
		return "required"
	case "minLength", "minimum", "exclusiveMinimum", "minItems", "minProperties":
		return "min"
	case "maxLength", "maximum", "exclusiveMaximum", "maxItems", "maxProperties":
		return "max"
	case "pattern":
		return "pattern"
	case "format":
		if err.Schema != nil {
			return err.Schema.Format
		}
	}
	return ""
}
