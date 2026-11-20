package httpserver

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

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
// them), so an unregistered `format: email` silently accepts anything.
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
		if !isEmailAddress(v) {
			return errors.New("not a valid email address")
		}
		return nil
	}))
})

// emailAddress is the WHATWG rule for <input type=email>, so the server
// accepts what a browser's email field does; contract/testdata/email.json pins
// its answers. No part of it can match the same text two ways, so it stays
// linear on a backtracking engine too.
var emailAddress = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+" +
	`@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

func isEmailAddress(v string) bool {
	return emailAddress.MatchString(strings.TrimSpace(v))
}

// registerStrictJSONDecoder replaces kin-openapi's application/json decoder,
// which stops after the first JSON value and lets encoding/json swap invalid
// UTF-8 for U+FFFD. A body with anything after the value, or one that is not
// UTF-8 JSON text, is INVALID_JSON_BODY instead. A byte order mark, and the
// NULs of a UTF-16 or UTF-32 body, fail encoding/json as stray characters, and
// the charset a Content-Type claims is never consulted, however unknown its
// name. Like the email format, the registry is process-global.
var registerStrictJSONDecoder = sync.OnceFunc(func() {
	openapi3filter.RegisterBodyDecoder("application/json", strictJSONBodyDecoder)
})

func strictJSONBodyDecoder(body io.Reader, _ http.Header, _ *openapi3.SchemaRef, _ openapi3filter.EncodingFn) (any, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, &openapi3filter.ParseError{Kind: openapi3filter.KindInvalidFormat, Cause: err}
	}
	if !utf8.Valid(data) {
		return nil, &openapi3filter.ParseError{Kind: openapi3filter.KindInvalidFormat, Reason: "body is not valid UTF-8"}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		// A syntax error's text quotes the offending character, and in a login
		// body that can be one from the password: its offset is enough.
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return nil, &openapi3filter.ParseError{Kind: openapi3filter.KindInvalidFormat, Reason: fmt.Sprintf("malformed JSON at offset %d", syntaxErr.Offset)}
		}
		return nil, &openapi3filter.ParseError{Kind: openapi3filter.KindInvalidFormat, Cause: err}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, &openapi3filter.ParseError{Kind: openapi3filter.KindInvalidFormat, Reason: "data after the JSON value"}
	}
	return value, nil
}

// openapiRequestValidator builds the middleware from the embedded spec
// (api.GetSpec, generated via oapi-codegen.yaml's embedded-spec: true —
// never hand-patch api.gen.go to add this; the generator config already
// owns it).
//
// AuthenticationFunc is a no-op: the contract's `security` is enforced by
// sessionRequired (session_required.go), which runs ahead of this middleware
// and reads the same requirements. This layer validates request shape only; a
// real AuthenticationFunc here would be a second enforcement point with its
// own opinion of what the requirements mean.
//
// MultiError is on so that every failure is collected and the reported one
// is chosen by the rule in writeOpenAPIValidationError, not whichever check
// kin-openapi happens to run first.
func openapiRequestValidator() func(http.Handler) http.Handler {
	registerEmailFormat()
	registerStrictJSONDecoder()

	spec, err := api.GetSpec()
	if err != nil {
		// Only reachable if the embedded spec itself is corrupt, which means
		// oapi-codegen's own generation was broken — every other generated
		// type would be suspect too. Nothing about this process is trustworthy.
		panic("httpserver: load embedded openapi spec: " + err.Error())
	}
	withoutSecurityRequirements(spec)

	validate := nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
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
	return func(next http.Handler) http.Handler {
		validated := validate(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			canonicalMediaType(r.Header)
			validated.ServeHTTP(w, r)
		})
	}
}

// withoutSecurityRequirements removes every security requirement from the
// validator's copy of the spec (api.GetSpec decodes a fresh one per call).
// sessionRequired enforces them before this middleware runs, so they check
// nothing here - and kin-openapi pays for them anyway: before it calls even a
// no-op AuthenticationFunc it reads the whole request body into memory, on
// every operation that declares one. Under maxBodyBytes a body over 1 MiB
// fails that read, the failure comes back as a SecurityRequirementsError, and
// a logout or a signed-in /me answered 401 for a body neither handler reads
// (#56). With no requirement left, kin-openapi returns before the read.
func withoutSecurityRequirements(spec *openapi3.T) {
	spec.Security = nil
	for _, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			op.Security = nil
		}
	}
}

// canonicalMediaType lowercases a Content-Type's type and subtype and drops
// the whitespace around them. kin-openapi finds both the operation's content
// entry and its body decoder by comparing that part byte for byte, so
// "Application/JSON" or "application/json ; charset=utf-8" was refused as
// INVALID_JSON_BODY. HTTP calls those the same media type (RFC 9110 §8.3.1).
// The parameters are left for the decoder, which ignores them.
//
// ASCII letters only: a token is ASCII (RFC 9110 §5.6.2), and strings.ToLower
// also folds U+0130 to "i", which would make "applİcation/json" JSON.
//
// A media type that is not token "/" token at all - "applİcation/json"
// included - is no media type, and the header is dropped: a route with a body
// then refuses it exactly as it refuses a missing Content-Type, and a route
// without one never looked. Done here rather than left to kin-openapi, whose
// own matching folds Unicode from v0.150.0 on and would read that spelling as
// application/json (#89).
func canonicalMediaType(h http.Header) {
	contentType := h.Get("Content-Type")
	if contentType == "" {
		return
	}
	mediaType, params, hasParams := strings.Cut(contentType, ";")
	mediaType = strings.Trim(mediaType, " \t")
	if !isTokenSlashToken(mediaType) {
		h.Del("Content-Type")
		return
	}
	canonical := strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, mediaType)
	if hasParams {
		canonical += ";" + params
	}
	h.Set("Content-Type", canonical)
}

// isTokenSlashToken reports whether s is RFC 9110 §8.3.1's type "/" subtype,
// each a non-empty token of tchar (§5.6.2) - ASCII only.
func isTokenSlashToken(s string) bool {
	typ, sub, ok := strings.Cut(s, "/")
	return ok && isToken(typ) && isToken(sub)
}

func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// writeOpenAPIValidationError translates the middleware's error into the
// shared envelope:
//
//   - A body that is not an instance of the declared shape at all — malformed
//     JSON, a wrong type, a null, a property the contract does not declare —
//     is INVALID_JSON_BODY, whatever else is wrong with it. The generated
//     decode answers the same code (requestErrorHandler).
//   - Otherwise every failure is a named field breaking a constraint, and the
//     one reported is the least by (field, rule) (#13 §3.5), which kin-openapi's own order — present properties, sorted, then
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
		// Unreachable: withoutSecurityRequirements leaves nothing to fail,
		// and the session check is sessionRequired's. Kept so a change that
		// restores the requirements degrades to the right code instead of a
		// 500 below. Before #56 this was reached, by a body read failing
		// inside the security check, not by a session.
		slog.Warn("openapi validator: unexpected security failure", "path", r.URL.Path)
		httperr.Write(w, http.StatusUnauthorized, httperr.CodeUnauthorized, nil)
		return
	}

	var found []failure
	if fault := collectFailures(err, &found); fault != nil || len(found) == 0 {
		// Not a verdict on the request: kin-openapi's router could not find the
		// operation chi had already matched, or validation itself broke. Either
		// way the fault is ours, and blaming the client's body would hide it.
		if fault == nil {
			fault = fmt.Errorf("no failure found in %T", err)
		}
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
			collectParameterFailures(e.Parameter.Name, e.Err, found)
			return nil
		}
		if e.RequestBody != nil && e.Err != nil {
			collectBodyFailures(e.Err, found)
			return nil
		}
		// A Content-Type the operation does not list. kin-openapi's text for it
		// quotes the header, never the body.
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
		// The body did not parse (a *ParseError, which strictJSONBodyDecoder
		// keeps to an offset), was empty (ErrInvalidRequired), or could not be
		// read at all, e.g. cut short by maxBodyBytes.
		*found = append(*found, failure{undecodable: err.Error()})
	}
}

// collectParameterFailures records one failure per rule a parameter breaks,
// so that two rules failing on one parameter are sorted like two on a body
// field rather than reported in kin-openapi's order.
func collectParameterFailures(name string, err error, found *[]failure) {
	if me, ok := err.(openapi3.MultiError); ok {
		for _, inner := range me {
			collectParameterFailures(name, inner, found)
		}
		return
	}
	*found = append(*found, failure{field: name, rule: parameterRule(err)})
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
// httperr.WriteValidation already uses — go-playground/validator's tag names,
// plus pattern — or "" for a keyword that means the body is not an instance of
// the declared shape (type, nullable, additionalProperties, enum, ...), which
// is INVALID_JSON_BODY, not VALIDATION. The same goes for every format but
// email: a bad date, date-time or uuid is a value of the wrong type, not a
// constraint failure.
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
		if err.Schema != nil && err.Schema.Format == "email" {
			return "email"
		}
	}
	return ""
}
