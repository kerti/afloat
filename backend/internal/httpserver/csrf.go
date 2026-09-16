package httpserver

import (
	"net/http"
	"net/url"

	"github.com/kerti/afloat/backend/internal/httperr"
)

// crossSiteGuard is the second CSRF layer, behind SameSite=Lax.
//
// Lax already stops a cross-site POST carrying the session cookie in every
// current browser. This covers what it does not: an older browser that ignores
// SameSite, and the same-site-but-different-origin case Lax treats as same-site.
// It costs one header read and needs no token plumbing or per-form state.
//
// Safe methods pass: they do not mutate, and blocking a cross-site GET would
// break ordinary navigation.
func crossSiteGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		// Sec-Fetch-Site is the browser's own statement about the request and
		// cannot be set by page script. Preferred when present.
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
			// `none` is a direct navigation or a tool with no originating site.
			if site != "same-origin" && site != "none" {
				httperr.Write(w, http.StatusForbidden, httperr.CodeCrossSiteRequestBlocked, nil)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		// Fall back to Origin for clients that send no Sec-Fetch-Site.
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.Host != r.Host {
				httperr.Write(w, http.StatusForbidden, httperr.CodeCrossSiteRequestBlocked, nil)
				return
			}
		}

		// Neither header present: not a browser form post, so there is no
		// ambient cookie to abuse. Curl and the test suite land here.
		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}
