package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/newsletter"
	"github.com/embiem/indie-game-gems/util"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// fakeUnsubscribeStore satisfies newsletter.UnsubscribeStore in memory.
type fakeUnsubscribeStore struct {
	subs map[string]data.Subscriber // keyed by canonical uuid string
}

func (f *fakeUnsubscribeStore) GetSubscriberByID(_ context.Context, id pgtype.UUID) (data.Subscriber, error) {
	sub, ok := f.subs[id.String()]
	if !ok {
		return data.Subscriber{}, pgx.ErrNoRows
	}
	return sub, nil
}

func (f *fakeUnsubscribeStore) MarkSubscriberUnsubscribed(_ context.Context, id pgtype.UUID) (data.Subscriber, error) {
	sub, ok := f.subs[id.String()]
	if !ok {
		return data.Subscriber{}, pgx.ErrNoRows
	}
	sub.Status = "unsubscribed"
	sub.UnsubscribedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	f.subs[id.String()] = sub
	return sub, nil
}

// TestOneClickUnsubscribeWithoutOrigin pins the RFC 8058 contract end to
// end through the REAL middleware stack (SameOriginOnly included): the
// mailbox provider's POST carries no Origin and no Sec-Fetch-Site header,
// must not be redirected, and must flip the subscriber to unsubscribed.
func TestOneClickUnsubscribeWithoutOrigin(t *testing.T) {
	const secret = "test-secret"
	store := &fakeUnsubscribeStore{subs: map[string]data.Subscriber{
		"0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001": {ID: pgtype.UUID{Bytes: newsletter.MustUUIDBytes("0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"), Valid: true}, Email: "reader@example.com", Status: "confirmed"},
	}}
	token := newsletter.NewUnsubToken(secret, "0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001")

	r := chi.NewRouter()
	r.Use(util.SameOriginOnly) // the middleware under test
	r.Post("/newsletter/unsubscribe/{token}", Make(func(w http.ResponseWriter, req *http.Request) error {
		return postUnsubscribe(w, req, store, secret)
	}))

	req := httptest.NewRequest(http.MethodPost, "/newsletter/unsubscribe/"+token,
		strings.NewReader("List-Unsubscribe=One-Click"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Deliberately NO Origin and NO Sec-Fetch-Site: what mailbox providers send.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("one-click POST status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("one-click POST must not redirect, got Location %q", loc)
	}
	if got := store.subs["0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"].Status; got != "unsubscribed" {
		t.Fatalf("subscriber status = %q, want unsubscribed", got)
	}
}

// The browser form on the GET page posts with Sec-Fetch-Site: same-origin —
// same handler, same 200.
func TestUnsubscribeFromOwnPage(t *testing.T) {
	const secret = "test-secret"
	store := &fakeUnsubscribeStore{subs: map[string]data.Subscriber{
		"0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001": {ID: pgtype.UUID{Bytes: newsletter.MustUUIDBytes("0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001"), Valid: true}, Email: "reader@example.com", Status: "confirmed"},
	}}
	token := newsletter.NewUnsubToken(secret, "0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001")

	r := chi.NewRouter()
	r.Use(util.SameOriginOnly)
	r.Post("/newsletter/unsubscribe/{token}", Make(func(w http.ResponseWriter, req *http.Request) error {
		return postUnsubscribe(w, req, store, secret)
	}))

	req := httptest.NewRequest(http.MethodPost, "/newsletter/unsubscribe/"+token, nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", "http://"+req.Host)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("same-origin form POST status = %d, want 200", rec.Code)
	}
}

// A forged cross-site POST is rejected by the middleware (defense in depth
// behind the token).
func TestUnsubscribeCrossSiteRejected(t *testing.T) {
	const secret = "test-secret"
	store := &fakeUnsubscribeStore{subs: map[string]data.Subscriber{}}
	token := newsletter.NewUnsubToken(secret, "0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001")

	r := chi.NewRouter()
	r.Use(util.SameOriginOnly)
	r.Post("/newsletter/unsubscribe/{token}", Make(func(w http.ResponseWriter, req *http.Request) error {
		return postUnsubscribe(w, req, store, secret)
	}))

	req := httptest.NewRequest(http.MethodPost, "/newsletter/unsubscribe/"+token, nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST status = %d, want 403", rec.Code)
	}
}

// Unknown or tampered tokens 404 without leaking which case it was.
func TestUnsubscribeBadToken(t *testing.T) {
	const secret = "test-secret"
	store := &fakeUnsubscribeStore{subs: map[string]data.Subscriber{}}

	r := chi.NewRouter()
	r.Post("/newsletter/unsubscribe/{token}", Make(func(w http.ResponseWriter, req *http.Request) error {
		return postUnsubscribe(w, req, store, secret)
	}))

	for _, token := range []string{"garbage", newsletter.NewUnsubToken("other-secret", "0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001")} {
		req := httptest.NewRequest(http.MethodPost, "/newsletter/unsubscribe/"+token, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("token %q status = %d, want 404", token, rec.Code)
		}
	}
}

// The per-address confirmation limiter allows the first send, throttles the
// immediate retry, and lets the window lapse.
func TestEmailLimiter(t *testing.T) {
	l := newEmailLimiter(15 * time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }

	if !l.allow("a@example.com") {
		t.Fatal("first send refused")
	}
	if l.allow("a@example.com") {
		t.Fatal("immediate re-send allowed")
	}
	if !l.allow("b@example.com") {
		t.Fatal("other address throttled")
	}
	now = now.Add(15 * time.Minute)
	if !l.allow("a@example.com") {
		t.Fatal("send after window refused")
	}
}
