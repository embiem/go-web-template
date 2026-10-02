package igdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient wires a Client at a fake Twitch auth endpoint + IGDB base.
func newTestClient(t *testing.T, auth, base *httptest.Server, lim time.Duration) *Client {
	t.Helper()
	c := NewClient(Credentials{ClientID: "cid", ClientSecret: "secret"})
	c.auth = auth.URL
	c.base = base.URL
	c.lim = time.Tick(lim)
	return c
}

func TestFromEnv(t *testing.T) {
	if _, err := FromEnv(func(string) string { return "" }); err != ErrNoCredentials {
		t.Errorf("empty env: got %v", err)
	}
	if _, err := FromEnv(func(k string) string { return map[string]string{"IGDB_CLIENT_ID": "a", "IGDB_CLIENT_SECRET": "b"}[k] }); err != nil {
		t.Errorf("full env: got %v", err)
	}
}

func TestTokenFetchAndCache(t *testing.T) {
	var authCalls atomic.Int32
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCalls.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("token method = %s", r.Method)
		}
		q := r.URL.Query()
		if q.Get("client_id") != "cid" || q.Get("grant_type") != "client_credentials" {
			t.Errorf("token params: %v", q)
		}
		w.Write([]byte(`{"access_token":"tok1","expires_in":3600,"token_type":"bearer"}`))
	}))
	defer auth.Close()

	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Client-ID") != "cid" {
			t.Errorf("missing Client-ID header")
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer tok1" {
			t.Errorf("Authorization = %q", auth)
		}
		w.Write([]byte(`[{"id":1}]`))
	}))
	defer base.Close()

	c := newTestClient(t, auth, base, time.Millisecond)
	for range 3 {
		var out []map[string]any
		if err := c.Query(t.Context(), "games", "fields id;", &out); err != nil {
			t.Fatal(err)
		}
	}
	if n := authCalls.Load(); n != 1 {
		t.Errorf("token fetched %d times, want 1 (must cache until expiry)", n)
	}
}

func TestTokenRefreshAfterExpiry(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"access_token":"tok1","expires_in":1,"token_type":"bearer"}`))
	}))
	defer auth.Close()
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	}))
	defer base.Close()

	c := newTestClient(t, auth, base, time.Millisecond)
	var out []map[string]any
	_ = c.Query(t.Context(), "games", "fields id;", &out)
	c.mu.Lock()
	c.tok.Expiry = time.Now().Add(-time.Hour) // force expiry
	c.mu.Unlock()
	_ = c.Query(t.Context(), "games", "fields id;", &out) // must re-fetch, not panic
}

func TestQueryBodyAndError(t *testing.T) {
	var gotBody string
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer base.Close()
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
	}))
	defer auth.Close()

	c := newTestClient(t, auth, base, time.Millisecond)
	var out []map[string]any
	err := c.Query(t.Context(), "games", "fields id; limit 500;", &out)
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected 429 error, got %v", err)
	}
	if gotBody != "fields id; limit 500;" {
		t.Errorf("apicalypse body = %q", gotBody)
	}
}

const fixtureGame = `[
 {
  "id": 1146,
  "name": "Hades",
  "slug": "hades",
  "summary": "Rogue-like dungeon crawler.",
  "first_release_date": 1600300800,
  "game_type": 0,
  "game_status": 0,
  "cover": {"image_id": "co1ji5", "width": 600, "height": 900},
  "screenshots": [{"image_id": "sc1"}, {"image_id": "sc2"}],
  "videos": [{"id": 1, "name": "Launch Trailer", "video_id": "dQw4w9WgXcQ"},
             {"id": 2, "name": "Gameplay", "video_id": "abc123"}],
  "websites": [
    {"id": 1, "url": "https://www.supergiantgames.com", "type": 1},
    {"id": 2, "url": "https://www.gog.com/en/game/hades", "type": 17},
    {"id": 3, "url": "https://store.epicgames.com/p/hades", "type": 16}
  ],
  "involved_companies": [
    {"company": {"id": 100, "name": "Supergiant Games", "slug": "supergiant-games",
                 "description": "Studio.", "logo": {"image_id": "cl1"},
                 "websites": [{"url": "https://www.supergiantgames.com", "type": 1}]},
     "developer": true, "publisher": true}
  ],
  "external_games": [
    {"id": 9, "uid": "1145360", "external_game_source": 1, "url": "https://store.steampowered.com/app/1145360", "game": 1146}
  ]
 }]`

func newGameServer(t *testing.T) (*httptest.Server, *[]string) {
	bodies := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		*bodies = append(*bodies, string(body[:n]))
		if strings.Contains(r.URL.Path, "external_games") {
			w.Write([]byte(`[{"id":9,"uid":"1145360","external_game_source":1,"game":1146}]`))
			return
		}
		w.Write([]byte(fixtureGame))
	}))
	t.Cleanup(srv.Close)
	return srv, bodies
}

func TestGameBySteamAppID(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
	}))
	defer auth.Close()
	srv, bodies := newGameServer(t)
	c := newTestClient(t, auth, srv, time.Millisecond)

	g, err := c.GameBySteamAppID(t.Context(), 1145360)
	if err != nil {
		t.Fatal(err)
	}
	if g.Name != "Hades" || g.Slug != "hades" {
		t.Fatalf("game: %+v", g)
	}
	// First request must be the external_games lookup with a quoted string uid.
	if !strings.Contains((*bodies)[0], `uid = "1145360"`) || !strings.Contains((*bodies)[0], "external_game_source = 1") {
		t.Errorf("external_games query = %q", (*bodies)[0])
	}
	if g.ReleaseDate() == nil || g.ReleaseDate().Year() != 2020 {
		t.Errorf("release date: %v", g.ReleaseDate())
	}
	if got := g.YouTubeTrailer(); got != "dQw4w9WgXcQ" {
		t.Errorf("trailer = %q", got)
	}
	links := g.StoreLinks()
	if links["gog"] == "" || links["epic"] == "" || links["steam"] == "" {
		t.Errorf("store links: %v", links)
	}
	if links["direct"] != "" {
		t.Errorf("official site must not map to direct: %v", links)
	}
	if g.OfficialWebsite() != "https://www.supergiantgames.com" {
		t.Errorf("official website: %q", g.OfficialWebsite())
	}
	if len(g.InvolvedCompanies) != 1 || g.InvolvedCompanies[0].Company.Slug != "supergiant-games" {
		t.Errorf("companies: %+v", g.InvolvedCompanies)
	}
	if g.Cover == nil || g.Cover.ImageID != "co1ji5" {
		t.Errorf("cover: %+v", g.Cover)
	}
}

func TestImageURL(t *testing.T) {
	got := ImageURL("co1ji5", SizeCoverBig)
	if got != "https://images.igdb.com/igdb/image/upload/t_cover_big/co1ji5.jpg" {
		t.Errorf("ImageURL = %q", got)
	}
}

func TestCompanyCatalogue(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
	}))
	defer auth.Close()

	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		lastBody = string(body[:n])
		// Two pages: full page then short page — pagination must continue.
		if strings.Contains(lastBody, "offset 0;") {
			w.Write([]byte(`[` + strings.TrimSuffix(strings.Repeat(`{"id":1,"name":"G","slug":"g"},`, 500), ",") + `]`))
			return
		}
		w.Write([]byte(`[{"id":2,"name":"G2","slug":"g2"}]`))
	}))
	defer srv.Close()

	c := newTestClient(t, auth, srv, time.Millisecond)
	games, err := c.CompanyCatalogue(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 501 {
		t.Fatalf("games = %d, want 501", len(games))
	}
	if !strings.Contains(lastBody, "involved_companies.developer = true") ||
		!strings.Contains(lastBody, "game_type != (1,2,3,5,6,7,13,14)") ||
		!strings.Contains(lastBody, "game_status != 6") {
		t.Errorf("catalogue query missing filters: %q", lastBody)
	}
}

func TestGameBySteamAppIDNotFound(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
	}))
	defer auth.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := newTestClient(t, auth, srv, time.Millisecond)
	if _, err := c.GameBySteamAppID(t.Context(), 1); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestFixtureJSONParses(t *testing.T) {
	var games []Game
	if err := json.Unmarshal([]byte(fixtureGame), &games); err != nil {
		t.Fatal(err)
	}
}
