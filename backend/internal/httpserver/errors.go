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
// non-negotiable 7 forbid, and which the Kotlin backend answers as an envelope.
//
// They are wired here rather than left at their defaults because a default that
// is wrong is invisible: nothing in the contract describes these paths, so no
// conformance test reaches them.

// requestErrorHandler covers a body the generated code could not decode —
// malformed JSON, or a body cut short by the 1 MiB cap in maxBodyBytes, which
// surfaces as a read error mid-decode.
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

// paramErrorHandler covers path, query, header and cookie binding. No endpoint
// in the contract declares such a parameter yet, so this is unreachable today
// and wired anyway: the first one that lands must not reopen the plain-text
// default, and this is not the kind of thing anyone remembers to revisit.
//
// The errors carry the parameter name, so the envelope can name the field the
// same way a body validation failure does.
func paramErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
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

// paramFieldAndRule reports the offending parameter and the rule vocabulary the
// frontend's catalogue uses — the same two keys, spelled the same way, as
// WriteValidation and the Kotlin ApiExceptionHandler.
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
