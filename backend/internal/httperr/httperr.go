package httperr

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"
)

// Envelope is the wire shape. Args is flat — JSON primitives only, never
// nested — so the frontend can pass it straight to t(key, args).
type Envelope struct {
	Code Code           `json:"code"`
	Args map[string]any `json:"args,omitempty"`
}

// Write ends the response with status and a single envelope. Content-Type is
// set explicitly: http.Error would send text/plain with a trailing newline,
// which is wrong for JSON.
func Write(w http.ResponseWriter, status int, code Code, args map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(Envelope{Code: code, Args: args}); err != nil {
		// The status and headers are already sent, so this cannot become an
		// error response. Log it and let the client see a truncated body.
		slog.Error("write error envelope", "err", err)
	}
}

// WriteValidation maps a validator failure to VALIDATION with {field, rule}.
// Only the first failure is reported: the envelope stays flat, and a form that
// fixes one field at a time is the behaviour both backends must share.
func WriteValidation(w http.ResponseWriter, err error) {
	var verrs validator.ValidationErrors
	if !asValidationErrors(err, &verrs) || len(verrs) == 0 {
		Write(w, http.StatusBadRequest, CodeValidation, nil)
		return
	}
	first := verrs[0]
	Write(w, http.StatusBadRequest, CodeValidation, map[string]any{
		// The JSON field name, not the Go one: the frontend's catalogue and the
		// contract both speak snake_case.
		"field": jsonFieldName(first),
		"rule":  first.Tag(),
	})
}

func asValidationErrors(err error, target *validator.ValidationErrors) bool {
	v, ok := err.(validator.ValidationErrors)
	if ok {
		*target = v
	}
	return ok
}

// jsonFieldName prefers the struct's json tag, falling back to a lowercased
// field name. validator reports the Go field, which is never what the wire
// calls it.
func jsonFieldName(fe validator.FieldError) string {
	if name := fe.Field(); name != "" {
		return strings.ToLower(name)
	}
	return fe.StructField()
}
