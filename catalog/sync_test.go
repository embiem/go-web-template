package catalog

import (
	"testing"

	"github.com/embiem/indie-game-gems/data"
	"github.com/embiem/indie-game-gems/igdb"
	"github.com/embiem/indie-game-gems/steam"
	"github.com/jackc/pgx/v5/pgtype"
)

func testGame() data.Game {
	return data.Game{
		ID:   pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		Slug: "hades",
	}
}

func int2(v int16) pgtype.Int2  { return pgtype.Int2{Int16: v, Valid: true} }
func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

func hadesDetails() *steam.AppDetails {
	d := &steam.AppDetails{
		Name:                "Hades",
		Website:             "https://www.supergiantgames.com",
		ShortDescription:    "Escape the underworld.",
		DetailedDescription: "<h2>About</h2><p>Battle out of <strong>Tartarus</strong>.</p><img src='x.png'/>",
	}
	d.ReleaseDate.Date = "Sep 17, 2020"
	return d
}

func TestSteamImportParams(t *testing.T) {
	game := testGame()
	params := SteamImportParams(game, hadesDetails(), &steam.ReviewSummary{
		TotalPositive: 97, TotalReviews: 100,
	})

	if params.ID != game.ID {
		t.Errorf("game id mismatch")
	}
	// HTML must be converted to markdown, images dropped.
	if params.DescriptionMd == "" || containsAny(params.DescriptionMd, "<h2>", "<img", "<strong>") {
		t.Errorf("description not converted: %q", params.DescriptionMd)
	}
	if !containsAny(params.DescriptionMd, "## About", "**Tartarus**") {
		t.Errorf("markdown content missing: %q", params.DescriptionMd)
	}
	// Review pct rounded: 97%.
	if !params.SteamReviewPct.Valid || params.SteamReviewPct.Int16 != 97 {
		t.Errorf("pct = %+v", params.SteamReviewPct)
	}
	if params.SteamReviewCount != 100 {
		t.Errorf("count = %d", params.SteamReviewCount)
	}
	// Verified release date parses through.
	if !params.ReleaseDate.Valid || params.ReleaseDate.Time.Month() != 9 {
		t.Errorf("release date = %+v", params.ReleaseDate)
	}
	if params.ReleaseStatus != "released" {
		t.Errorf("status = %q", params.ReleaseStatus)
	}
}

func TestSteamImportParamsNilReviews(t *testing.T) {
	params := SteamImportParams(testGame(), hadesDetails(), nil)
	if params.SteamReviewPct.Valid || params.SteamReviewCount != 0 {
		t.Errorf("nil reviews must leave stats empty: %+v", params.SteamReviewPct)
	}
}

func TestSteamImportParamsBadDescriptionFallsBack(t *testing.T) {
	game := testGame()
	game.DescriptionMd = "existing"
	d := hadesDetails()
	d.DetailedDescription = "<unclosed" // unparseable-ish HTML

	// Converter still yields something or the existing description wins.
	params := SteamImportParams(game, d, nil)
	if params.DescriptionMd == "" {
		t.Errorf("description must never become empty")
	}
}

func TestIGDBGameParamsCuratedWins(t *testing.T) {
	game := testGame()
	game.IgdbID = pgtype.Int8{Int64: 999, Valid: true}
	game.TrailerYoutubeID = text("existing")

	params := IGDBGameParams(game, &igdb.Game{ID: 1234})
	if params.IgdbID.Valid {
		t.Errorf("existing igdb_id must win, got %+v", params.IgdbID)
	}
	// Existing trailer: params stay zero (NULL); the SQL layer COALESCEs and
	// keeps the curated value.
	if params.TrailerYoutubeID.Valid {
		t.Errorf("existing trailer must be left to SQL COALESCE, got %+v", params.TrailerYoutubeID)
	}
}

func TestIGDBGameParamsFillsGaps(t *testing.T) {
	g := &igdb.Game{
		ID: 1234,
		Videos: []igdb.Video{
			{Name: "Gameplay", VideoID: "play1"},
			{Name: "Official Launch Trailer", VideoID: "trail1"},
		},
	}
	params := IGDBGameParams(testGame(), g)
	if !params.IgdbID.Valid || params.IgdbID.Int64 != 1234 {
		t.Errorf("igdb_id = %+v", params.IgdbID)
	}
	// "Trailer"-named video preferred over first video.
	if params.TrailerYoutubeID.String != "trail1" {
		t.Errorf("trailer = %q", params.TrailerYoutubeID.String)
	}

	// No videos -> no trailer.
	g.Videos = nil
	if p := IGDBGameParams(testGame(), g); p.TrailerYoutubeID.Valid {
		t.Errorf("no videos must leave trailer NULL, got %q", p.TrailerYoutubeID.String)
	}
}

func TestMediaJobsSteam(t *testing.T) {
	row := data.ListGamesForSyncRow{
		ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Slug: "hades",
		SteamAppid: pgtype.Int8{Int64: 1145360, Valid: true},
	}
	details := hadesDetails()
	details.HeaderImage = "https://x/header.jpg"
	details.CapsuleImage = "https://x/capsule_231x87.jpg"
	details.Screenshots = make([]struct {
		ID            int    `json:"id"`
		PathThumbnail string `json:"path_thumbnail"`
		PathFull      string `json:"path_full"`
	}, 10)
	for i := range details.Screenshots {
		details.Screenshots[i].PathFull = "https://x/shot.jpg"
	}

	jobs := MediaJobs(row, details, nil)
	kinds := map[string]int{}
	for _, j := range jobs {
		kinds[j.kind]++
	}
	if kinds["header"] != 1 || kinds["capsule"] != 1 || kinds["hero"] != 1 || kinds["cover"] != 1 {
		t.Errorf("steam kinds: %+v", kinds)
	}
	if kinds["screenshot"] != maxScreenshots {
		t.Errorf("screenshots capped at %d, got %d", maxScreenshots, kinds["screenshot"])
	}
	// Screenshot keys are positional.
	if jobs[len(jobs)-1].key != "games/hades/screenshot-08" {
		t.Errorf("last screenshot key = %q", jobs[len(jobs)-1].key)
	}
	if jobs[0].key != "games/hades/header" {
		t.Errorf("header key = %q", jobs[0].key)
	}
}

func TestMediaJobsIGDBFallbackAndTrailer(t *testing.T) {
	row := data.ListGamesForSyncRow{
		ID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, Slug: "uncurated",
		TrailerYoutubeID: text("abc123"),
	}
	g := &igdb.Game{
		Cover:       &igdb.Image{ImageID: "co1"},
		Screenshots: []igdb.Image{{ImageID: "s1"}, {ImageID: "s2"}},
	}
	jobs := MediaJobs(row, nil, g)
	var keys []string
	for _, j := range jobs {
		keys = append(keys, j.key)
	}
	want := []string{"games/uncurated/cover", "games/uncurated/screenshot-01", "games/uncurated/screenshot-02", "games/uncurated/trailer"}
	if len(keys) != len(want) {
		t.Fatalf("keys = %v", keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("key[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
	if jobs[0].url != "https://images.igdb.com/igdb/image/upload/t_cover_big/co1.jpg" {
		t.Errorf("igdb cover url = %q", jobs[0].url)
	}
	if jobs[len(jobs)-1].url != "https://i.ytimg.com/vi/abc123/hqdefault.jpg" {
		t.Errorf("trailer poster url = %q", jobs[len(jobs)-1].url)
	}
}

func TestSteamLibraryURL(t *testing.T) {
	got := steamLibraryURL(1145360, "library_600x900")
	if got != "https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/1145360/library_600x900.jpg" {
		t.Errorf("steamLibraryURL = %q", got)
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Supergiant Games":    "supergiant-games",
		"Team Cherry":         "team-cherry",
		"  Multiple   Spaces": "multiple-spaces",
		"École":               "cole",
		"Already-Slugged 2":   "already-slugged-2",
	}
	for in, want := range tests {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && indexOf(s, sub) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
