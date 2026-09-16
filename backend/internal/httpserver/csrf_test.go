package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCrossSiteGuard(t *testing.T) {
	reached := false
	guarded := crossSiteGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	for _, tc := range []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		// Safe methods pass regardless: they do not mutate, and blocking a
		// cross-site GET would break ordinary navigation.
		{"GET from another site", http.MethodGet, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusOK},
		{"HEAD from another site", http.MethodHead, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusOK},

		{"POST same-origin", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		// `none` is a direct navigation or a tool with no originating site.
		{"POST with no originating site", http.MethodPost, map[string]string{"Sec-Fetch-Site": "none"}, http.StatusOK},
		{"POST cross-site", http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		// same-site but a different origin is what SameSite=Lax lets through.
		{"POST same-site, other origin", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},

		{"POST matching Origin", http.MethodPost, map[string]string{"Origin": "http://example.com"}, http.StatusOK},
		{"POST foreign Origin", http.MethodPost, map[string]string{"Origin": "http://evil.example"}, http.StatusForbidden},
		{"POST unparseable Origin", http.MethodPost, map[string]string{"Origin": "://"}, http.StatusForbidden},

		// Neither header: not a browser form post, so no ambient cookie to
		// abuse. curl and the test suite land here.
		{"POST with neither header", http.MethodPost, nil, http.StatusOK},

		{"DELETE cross-site", http.MethodDelete, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"PATCH cross-site", http.MethodPatch, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reached = false
			req := httptest.NewRequest(tc.method, "http://example.com/api/auth/logout", nil)
			req.Host = "example.com"
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}

			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			if tc.want == http.StatusForbidden && reached {
				t.Error("the handler ran despite the request being refused")
			}
		})
	}
}

// A refusal must carry the shared envelope, so the frontend can localise it
// rather than showing a bare 403.
func TestCrossSiteGuardUsesTheSharedEnvelope(t *testing.T) {
	guarded := crossSiteGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/auth/logout", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body["code"] != "CROSS_SITE_REQUEST_BLOCKED" {
		t.Errorf("code = %v, want CROSS_SITE_REQUEST_BLOCKED", body["code"])
	}
}
