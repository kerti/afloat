package httpserver

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/kerti/afloat/backend/internal/api"
	"github.com/kerti/afloat/backend/internal/httperr"
)

// The generated server has three escape hatches that run OUTSIDE any handler —
// body decoding, parameter binding, and a handler returning a non-nil error.
// All three default to http.Error, which writes text/plain carrying err.Error():
// an English message on the wire, which is exactly what PRD N7 and
// non-negotiable 7 forbid.
//
// They are wired here rather than left at their defaults because a default that
// is wrong is invisible: nothing in the contract describes these paths, so no
// conformance test reaches them.

// requestErrorHandler covers a body the generated code could not decode. The
// spec-validating middleware (openapi_validate.go) reads and checks every
// declared body first, so malformed JSON and a body cut short by maxBodyBytes'
// 1 MiB cap are answered there. What reaches this is a body that passes the
// contract and still fails the generated decode: a duplicate key whose earlier
// value has the wrong type. kin-openapi checks only the last value, where
// encoding/json type-checks every one. The error text names
// the JSON kind (number, array), never the value.
func requestErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	slog.Warn("request body rejected", "path", r.URL.Path, "err", err)
	httperr.Write(w, http.StatusBadRequest, httperr.CodeInvalidJSONBody, nil)
}

// responseErrorHandler covers a handler that returned an error rather than a
// response object. Handlers map their own failures to envelopes, so reaching
// here means something was missed: the cause is logged, the client is told
// nothing about it.
func responseErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("handler returned an error", "path", r.URL.Path, "err", err)
	httperr.Write(w, http.StatusInternalServerError, httperr.CodeInternal, nil)
}

// paramErrorHandlerFor covers path, query, header and cookie binding. The
// generated wrapper binds these at the top of each operation, BEFORE the
// per-operation middlewares run (oapi-codegen's chi-middleware.tmpl), so without
// the check below a malformed {id} would answer 400 ahead of sessionRequired's
// 401 and an unauthenticated caller would learn what the binder accepts. The
// session is therefore asked first, with the same decision sessionRequired makes
// (refusedForSession), and the parameter is reported only to a caller who may
// make the request (#121, BOOTSTRAP.md §6).
//
// The errors carry the parameter name, so the envelope can name the field the
// same way a body validation failure does. required is sessionRequirements' map
// of the contract.
func paramErrorHandlerFor(required map[string]bool) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if refusedForSession(w, r, required) {
			return
		}
		slog.Warn("request parameter rejected", "path", r.URL.Path, "err", err)

		field, rule := paramFieldAndRule(err)
		if field == "" {
			httperr.Write(w, http.StatusBadRequest, httperr.CodeValidation, nil)
			return
		}
		httperr.Write(w, http.StatusBadRequest, httperr.CodeValidation, map[string]any{
			"field": field,
			"rule":  rule,
		})
	}
}

// paramFieldAndRule reports the offending parameter and the rule vocabulary the
// frontend's catalogue uses — the same two keys, spelled the same way, as
// WriteValidation.
func paramFieldAndRule(err error) (string, string) {
	if e, ok := errors.AsType[*api.RequiredParamError](err); ok {
		return e.ParamName, "required"
	}
	if e, ok := errors.AsType[*api.RequiredHeaderError](err); ok {
		return e.ParamName, "required"
	}
	if e, ok := errors.AsType[*api.InvalidParamFormatError](err); ok {
		return e.ParamName, "invalid"
	}
	if e, ok := errors.AsType[*api.UnmarshalingParamError](err); ok {
		return e.ParamName, "invalid"
	}
	if e, ok := errors.AsType[*api.UnescapedCookieParamError](err); ok {
		return e.ParamName, "invalid"
	}
	if e, ok := errors.AsType[*api.TooManyValuesForParamError](err); ok {
		return e.ParamName, "invalid"
	}
	return "", ""
}
