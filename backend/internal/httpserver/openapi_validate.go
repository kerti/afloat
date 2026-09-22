package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
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
// the padded form is accepted at all.
//
// DefineStringFormatValidator is process-global state in kin-openapi, so this
// runs once via sync.OnceFunc rather than on every middleware build.
var registerEmailFormat = sync.OnceFunc(func() {
	openapi3.DefineStringFormatValidator("email", openapi3.NewCallbackValidator(func(v string) error {
		if err := httperr.Validator().Var(strings.TrimSpace(v), "email"); err != nil {
			return fmt.Errorf("not a valid email address")
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
// for itself whether it needs one (docs/adr/go/0001), so that a new
// authenticated route can't be added without a deliberate choice. Wiring a
// real AuthenticationFunc here would make the OpenAPI layer a second,
// earlier enforcement point with its own opinion, which disagrees with that
// design the moment the two diverge, and duplicates in kin-openapi terms
// exactly the check SessionMiddleware already owns.
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
		},
		// The servers entry is a bare path ("/api"), not a host: silencing this
		// just skips the (irrelevant) Host-header warning kin-openapi prints for
		// specs that declare a server at all.
		SilenceServersWarning: true,
		ErrorHandlerWithOpts:  writeOpenAPIValidationError,
	})
}

// writeOpenAPIValidationError translates the middleware's error into the
// shared envelope, matching the shape httperr.WriteValidation produces
// ({field, rule}, first failure only) without routing through it: that
// helper is typed to validator.ValidationErrors, and kin-openapi's errors are
// a structurally different type: forcing one into the other would be a
// stranger adapter than writing the same envelope directly through
// httperr.Write, the primitive both ultimately go through. Only the first
// failure is ever reported because MultiError is never enabled above, so
// openapi3filter.ValidateRequest already stops at the first failing check.
func writeOpenAPIValidationError(_ context.Context, err error, w http.ResponseWriter, r *http.Request, _ nethttpmiddleware.ErrorHandlerOpts) {
	var schemaErr *openapi3.SchemaError
	if errors.As(err, &schemaErr) {
		if field, rule := fieldAndRule(schemaErr); field != "" {
			httperr.Write(w, http.StatusBadRequest, httperr.CodeValidation, map[string]any{
				"field": field,
				"rule":  rule,
			})
			return
		}
		// No single field to blame — e.g. the body decoded as JSON but its root
		// isn't even an object (SchemaField "type", an empty JSONPointer: `["x"]`
		// or `"x"` sent where the contract declares an object). That's not a
		// field failing a constraint, it's the body not being an instance of
		// what the contract declares at all, so it falls into the same bucket
		// as a body that didn't decode below rather than a contentless
		// VALIDATION with no args to act on.
		slog.Warn("request body rejected by contract", "path", r.URL.Path, "err", err)
		httperr.Write(w, http.StatusBadRequest, httperr.CodeInvalidJSONBody, nil)
		return
	}

	var secErr *openapi3filter.SecurityRequirementsError
	if errors.As(err, &secErr) {
		// Unreachable while AuthenticationFunc above is the no-op: it never
		// fails a security requirement. Kept so a future change to that
		// function degrades to the right code instead of falling through to
		// INVALID_JSON_BODY below.
		slog.Warn("openapi validator: unexpected security failure", "path", r.URL.Path, "err", err)
		httperr.Write(w, http.StatusUnauthorized, httperr.CodeUnauthorized, nil)
		return
	}

	// Everything else here is "the body did not decode as what the contract
	// declares" — malformed JSON, a body that failed to parse, a Content-Type
	// the operation doesn't list, a required body that is empty. That is
	// exactly what requestErrorHandler (errors.go) already answers with
	// INVALID_JSON_BODY for the escape hatches this middleware does not sit in
	// front of; same code, same meaning, whichever half of the pipeline
	// catches it first.
	slog.Warn("request body rejected by contract", "path", r.URL.Path, "err", err)
	httperr.Write(w, http.StatusBadRequest, httperr.CodeInvalidJSONBody, nil)
}

// unsupportedPropertyPattern pulls the offending key out of kin-openapi's
// additionalProperties failure, whose only structured signal is
// SchemaField == "properties" with an empty JSONPointer — the property name
// only appears in Reason (openapi3/schema.go: `property %q is unsupported`).
// A future kin-openapi wording change just means the field is omitted from
// args, not that the request is wrongly accepted: the envelope still carries
// {"code":"VALIDATION"}.
var unsupportedPropertyPattern = regexp.MustCompile(`^property "([^"]+)" is unsupported$`)

// fieldAndRule maps a kin-openapi SchemaError onto the {field, rule}
// vocabulary httperr.WriteValidation and the frontend catalogue already
// share — go-playground/validator's own tag names (required, email, min,
// max) — so a VALIDATION envelope reads the same whether it came from a
// validator.Validate() call or, as here, the spec.
func fieldAndRule(err *openapi3.SchemaError) (field, rule string) {
	pointer := err.JSONPointer()

	switch err.SchemaField {
	case "required":
		return strings.Join(pointer, "."), "required"
	case "maxLength":
		return strings.Join(pointer, "."), "max"
	case "minLength":
		return strings.Join(pointer, "."), "min"
	case "format":
		if err.Schema != nil && err.Schema.Format != "" {
			return strings.Join(pointer, "."), err.Schema.Format
		}
		return strings.Join(pointer, "."), "format"
	case "properties":
		// additionalProperties: false — no JSONPointer to the extra key, so it
		// is pulled from the one place it is named.
		if m := unsupportedPropertyPattern.FindStringSubmatch(err.Reason); m != nil {
			return m[1], "unknown"
		}
		return "", ""
	default:
		// A constraint the contract does not use today (pattern, enum, min/max
		// numeric, ...). Report the field and the raw keyword rather than guess
		// at a vocabulary word the frontend catalogue may not have yet.
		if len(pointer) == 0 {
			return "", ""
		}
		return strings.Join(pointer, "."), err.SchemaField
	}
}
