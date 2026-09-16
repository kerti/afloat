package httperr

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// validate is the project's validator, configured once so that a failure
// reports the JSON field name rather than the Go one.
//
// This matters more than it looks: validator reports the struct field by
// default, so DisplayName becomes "displayname", and the frontend looks up a
// catalogue key that does not exist. The contract speaks snake_case and so must
// the envelope.
//
// Handlers must use Validator() rather than calling validator.New() themselves,
// or they lose this mapping.
var validate = func() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
		if name == "" || name == "-" {
			return f.Name
		}
		return name
	})
	return v
}()

// Validator returns the configured validator. Safe for concurrent use.
func Validator() *validator.Validate { return validate }

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
	// errors.As, not a type assertion: a handler that wraps the failure with
	// context would otherwise fall through to a bare VALIDATION with no field
	// or rule, which tells the frontend nothing.
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) || len(verrs) == 0 {
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

// jsonFieldName returns what the wire calls the field. With the tag-name func
// registered above, Field() is already the json name; the fallback covers a
// caller that built its own validator without it.
func jsonFieldName(fe validator.FieldError) string {
	if name := fe.Field(); name != "" {
		return name
	}
	return fe.StructField()
}
