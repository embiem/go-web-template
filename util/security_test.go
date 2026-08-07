package util

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOriginOnly(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		secFetch   string
		origin     string
		wantStatus int
	}{
		{"GET is never blocked", http.MethodGet, "cross-site", "", http.StatusOK},
		{"same-origin POST", http.MethodPost, "same-origin", "", http.StatusOK},
		{"typed-URL POST", http.MethodPost, "none", "", http.StatusOK},
		{"cross-site POST", http.MethodPost, "cross-site", "", http.StatusForbidden},
		{"sibling subdomain POST", http.MethodPost, "same-site", "", http.StatusForbidden},
		{"legacy browser, no headers", http.MethodPost, "", "", http.StatusOK},
		{"legacy browser, own origin", http.MethodPost, "", "http://example.com", http.StatusOK},
		{"legacy browser, foreign origin", http.MethodPost, "", "http://evil.test", http.StatusForbidden},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "http://example.com/login", nil)
			if tt.secFetch != "" {
				req.Header.Set("Sec-Fetch-Site", tt.secFetch)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			rec := httptest.NewRecorder()
			SameOriginOnly(next).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestSecurityHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	for _, hsts := range []bool{false, true} {
		rec := httptest.NewRecorder()
		SecurityHeaders(hsts)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
			if rec.Header().Get(h) == "" {
				t.Errorf("hsts=%v: missing %s", hsts, h)
			}
		}
		if got := rec.Header().Get("Strict-Transport-Security") != ""; got != hsts {
			t.Errorf("hsts=%v: HSTS header present = %v", hsts, got)
		}
	}
}
