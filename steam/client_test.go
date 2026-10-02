package steam

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func mustFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

func TestParseAppDetailsHades(t *testing.T) {
	var resp map[string]struct {
		Success bool        `json:"success"`
		Data    *AppDetails `json:"data"`
	}
	if err := json.Unmarshal(mustFixture(t, "steam_appdetails_1145360.json"), &resp); err != nil {
		t.Fatal(err)
	}
	d := resp["1145360"].Data
	if d == nil || d.Name != "Hades" || d.SteamAppID != 1145360 {
		t.Fatalf("bad data: %+v", d)
	}
	if d.ReleaseDate.Date != "Sep 17, 2020" || d.ReleaseDate.ComingSoon {
		t.Errorf("release_date: %+v", d.ReleaseDate)
	}
	if len(d.Screenshots) == 0 || d.Screenshots[0].PathFull == "" {
		t.Errorf("screenshots missing: %+v", d.Screenshots)
	}
	if d.HeaderImage == "" || d.CapsuleImage == "" {
		t.Errorf("images missing")
	}
	if len(d.Developers) == 0 || d.Developers[0] != "Supergiant Games" {
		t.Errorf("developers: %v", d.Developers)
	}
	if !strings.Contains(d.DetailedDescription, "<") {
		t.Errorf("expected HTML description")
	}
}

func TestParseAppDetailsComingSoon(t *testing.T) {
	var resp map[string]struct {
		Success bool        `json:"success"`
		Data    *AppDetails `json:"data"`
	}
	if err := json.Unmarshal(mustFixture(t, "steam_appdetails_2201700_comingsoon.json"), &resp); err != nil {
		t.Fatal(err)
	}
	d := resp["2201700"].Data
	if d == nil {
		t.Fatal("no data")
	}
	if !d.ReleaseDate.ComingSoon {
		t.Errorf("expected coming_soon")
	}
	if _, ok := ParseReleaseDate(d.ReleaseDate.Date, d.ReleaseDate.ComingSoon); ok {
		t.Errorf("sentinel %q should parse as unknown", d.ReleaseDate.Date)
	}
	if d.ReleaseStatus() != "upcoming" {
		t.Errorf("status = %q", d.ReleaseStatus())
	}
}

func TestParseReviewsHades(t *testing.T) {
	var resp struct {
		QuerySummary *ReviewSummary `json:"query_summary"`
	}
	if err := json.Unmarshal(mustFixture(t, "appreviews_1145360.json"), &resp); err != nil {
		t.Fatal(err)
	}
	s := resp.QuerySummary
	if s == nil || s.TotalReviews != 309089 || s.TotalPositive != 302905 {
		t.Fatalf("bad summary: %+v", s)
	}
	if pct := s.ReviewPct(); pct < 97 || pct > 99 {
		t.Errorf("pct = %v", pct)
	}
}

func TestClientFetchesFixtures(t *testing.T) {
	details := mustFixture(t, "steam_appdetails_1145360.json")
	reviews := mustFixture(t, "appreviews_1145360.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Errorf("missing User-Agent")
		}
		switch {
		case strings.HasPrefix(r.URL.RequestURI(), "/api/appdetails"):
			w.Write(details)
		case strings.HasPrefix(r.URL.RequestURI(), "/appreviews/"):
			w.Write(reviews)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.limiter = time.Tick(time.Millisecond) // tests don't need the 1 req/s pace
	d, err := c.AppDetails(t.Context(), 1145360)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Hades" {
		t.Errorf("name = %q", d.Name)
	}
	s, err := c.Reviews(t.Context(), 1145360)
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalReviews != 309089 {
		t.Errorf("total_reviews = %d", s.TotalReviews)
	}
}

func TestClientMissingApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"999999999":{"success":false}}`))
	}))
	defer srv.Close()
	if _, err := NewClient(srv.URL).AppDetails(t.Context(), 999999999); err == nil {
		t.Fatal("expected error for success:false without data")
	}
}

func TestClientRetriesOn500(t *testing.T) {
	// Shrink the limiter interval and backoff so the test is fast.
	c := NewClient("")
	c.limiter = time.Tick(time.Millisecond)
	c.backoff = time.Millisecond
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"42":{"success":true,"data":{"name":"Z"}}}`))
	}))
	defer srv.Close()
	c.baseURL = srv.URL

	d, err := c.AppDetails(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Z" || calls != 3 {
		t.Fatalf("name=%q calls=%d", d.Name, calls)
	}
}
