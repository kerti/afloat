package httperr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteEnvelopeShape(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, http.StatusTooManyRequests, CodeTooManyAttempts, map[string]any{"retry_after": 4})

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body["code"] != "TOO_MANY_ATTEMPTS" {
		t.Errorf("code = %v", body["code"])
	}
	if _, present := body["message"]; present {
		t.Error("envelope carries a message field; codes only (PRD N7)")
	}
}

// args is omitted rather than sent as null, so the client can branch on
// presence without a null check.
func TestWriteOmitsEmptyArgs(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, http.StatusUnauthorized, CodeUnauthorized, nil)

	if got := rec.Body.String(); got != `{"code":"UNAUTHORIZED"}`+"\n" {
		t.Errorf("body = %q, want no args key", got)
	}
}

type loginBody struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=10"`
}

func TestWriteValidationReportsFirstFieldAndRule(t *testing.T) {
	err := Validator().Struct(loginBody{Email: "not-an-email", Password: "short"})
	if err == nil {
		t.Fatal("expected the fixture to fail validation")
	}

	rec := httptest.NewRecorder()
	WriteValidation(rec, err)

	var body struct {
		Code string            `json:"code"`
		Args map[string]string `json:"args"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body.Code != "VALIDATION" {
		t.Errorf("code = %q, want VALIDATION", body.Code)
	}
	// First failure only: the envelope is flat, and both backends must agree on
	// which one is reported (BOOTSTRAP.md §5.2).
	if body.Args["field"] != "email" {
		t.Errorf("field = %q, want email (the first failing field)", body.Args["field"])
	}
	if body.Args["rule"] != "email" {
		t.Errorf("rule = %q, want email", body.Args["rule"])
	}
}

// A multi-word field must report its snake_case JSON name. "email" passes with
// or without the tag-name func registered, so it proves nothing on its own —
// this is the case that caught it.
func TestWriteValidationUsesJSONFieldNames(t *testing.T) {
	type body struct {
		DisplayName string `json:"display_name" validate:"required"`
	}

	rec := httptest.NewRecorder()
	WriteValidation(rec, Validator().Struct(body{}))

	var got struct {
		Args map[string]string `json:"args"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got.Args["field"] != "display_name" {
		t.Errorf("field = %q, want display_name (the contract speaks snake_case)", got.Args["field"])
	}
}

// A handler that adds context to the failure must not lose the field and rule.
func TestWriteValidationSeesThroughWrapping(t *testing.T) {
	wrapped := fmt.Errorf("decode login body: %w", Validator().Struct(loginBody{Email: "nope", Password: "short"}))

	rec := httptest.NewRecorder()
	WriteValidation(rec, wrapped)

	var got struct {
		Args map[string]string `json:"args"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got.Args["field"] != "email" || got.Args["rule"] != "email" {
		t.Errorf("args = %v, want field=email rule=email through the wrapping", got.Args)
	}
}

// A non-validator error must still produce the envelope rather than panicking
// on a failed type assertion.
func TestWriteValidationHandlesOtherErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteValidation(rec, http.ErrBodyNotAllowed)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
