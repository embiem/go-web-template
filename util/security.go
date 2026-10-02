package util

import (
	"net/http"
	"strings"
)

// csp matches what view/layout.templ actually loads: everything, including
// htmx and the trailer loader, is served same-origin from /public. The only
// third-party sink is the click-to-load YouTube trailer facade
// (youtube-nocookie iframes); img/style/font stay same-origin (data: lets
// small inline icons through).
const csp = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' data:; " +
	"frame-src https://www.youtube-nocookie.com; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// SecurityHeaders sets the baseline headers every response should carry. HSTS
// is only sent when the app is served over HTTPS.
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SameOriginOnly rejects state-changing requests the browser reports as coming
// from another origin. This is defense in depth behind the SameSite=Lax session
// cookie and costs no server-side state, unlike CSRF tokens.
//
// Note it also rejects "same-site" (a sibling subdomain); relax that case if
// you intentionally post to this app from another subdomain.
func SameOriginOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if !sameOrigin(r) {
				http.Error(w, "Cross-origin request rejected", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
		// Browser too old for Sec-Fetch-*: fall back to Origin when it's sent.
		origin := r.Header.Get("Origin")
		return origin == "" ||
			strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://") == r.Host
	default: // cross-site, same-site
		return false
	}
}
