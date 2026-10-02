package handler

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/embiem/indie-game-gems/view"
)

// TestSubscribeInstanceRouting pins the htmx contract for the embedded
// signup forms: the response partial must swap the slot the request came
// from (HX-Target), with that instance's DOM ids.
func TestSubscribeInstanceRouting(t *testing.T) {
	cases := []struct {
		target string
		wantID string
	}{
		{"newsletter-signup-footer", "newsletter-signup-footer"},
		{"newsletter-signup-home", "newsletter-signup-home"},
		{"", "newsletter-signup"}, // full form on /newsletter
	}
	for _, tc := range cases {
		req := httptest.NewRequest("POST", "/newsletter/subscribe", strings.NewReader("email=x@y.com"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		if tc.target != "" {
			req.Header.Set("HX-Target", tc.target)
		}
		rec := httptest.NewRecorder()
		if err := renderSubscribeResult(rec, req, view.NewsletterFormState{Kind: "success"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rec.Body.String(), `id="`+tc.wantID+`"`) {
			t.Fatalf("target %q: body lacks id %q: %.120s", tc.target, tc.wantID, rec.Body.String())
		}
	}
}

// The embedded forms post the same fields as the full form.
func TestSubscribeFormEncoding(t *testing.T) {
	form := url.Values{"email": {"a@b.com"}, "organization": {""}}
	req := httptest.NewRequest("POST", "/newsletter/subscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if req.PostFormValue("email") != "a@b.com" || req.PostFormValue("organization") != "" {
		t.Fatal("field parsing changed")
	}
}
