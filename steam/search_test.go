package steam

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseSearchResultsFixture(t *testing.T) {
	var resp struct {
		ResultsHTML string `json:"results_html"`
		TotalCount  int    `json:"total_count"`
	}
	if err := json.Unmarshal(mustFixture(t, "search_released_desc.json"), &resp); err != nil {
		t.Fatal(err)
	}
	rows, err := parseSearchResults(resp.ResultsHTML, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	want := SearchResult{
		AppID:       3501560,
		Name:        "Sweep The Underworld",
		ReleaseText: "Oct 1, 2026",
		ReleaseDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
	if rows[0] != want {
		t.Errorf("row 0 = %+v, want %+v", rows[0], want)
	}
	if resp.TotalCount == 0 {
		t.Error("total_count missing")
	}
}

func TestParseDayDate(t *testing.T) {
	for in, want := range map[string]string{
		"Oct 1, 2026":  "2026-10-01",
		"1 Oct, 2026":  "2026-10-01",
		"October 2026": "",
		"May 2026":     "",
		"Q4 2026":      "",
		"Coming soon":  "",
	} {
		got := parseDayDate(in)
		if (want == "" && !got.IsZero()) || (want != "" && got.Format(time.DateOnly) != want) {
			t.Errorf("parseDayDate(%q) = %v, want %q", in, got, want)
		}
	}
}

type fakeRow struct {
	appid int64
	date  string
}

// fakeSearch serves both listings in pages of pageSize rows (smaller than
// requested, like Steam sometimes does) and counts requests per listing.
type fakeSearch struct {
	released, comingSoon []fakeRow
	pageSize             int
	requests             map[string]int
	tags                 []string
}

func (f *fakeSearch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	listing, rows := "released", f.released
	if q.Get("filter") == "comingsoon" {
		listing, rows = "comingsoon", f.comingSoon
	}
	f.requests[listing]++
	f.tags = append(f.tags, q.Get("tags"))
	start, _ := strconv.Atoi(q.Get("start"))
	end := min(start+f.pageSize, len(rows))
	var b strings.Builder
	for _, row := range rows[min(start, end):end] {
		fmt.Fprintf(&b, `<a href="x" data-ds-appid="%d" class="search_result_row"><span class="title">Game %d</span><div class="search_released responsive_secondrow"> %s </div></a>`, row.appid, row.appid, row.date)
	}
	json.NewEncoder(w).Encode(map[string]any{"success": 1, "results_html": b.String(), "total_count": len(rows)})
}

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func appids(rows []SearchResult) []int64 {
	var out []int64
	for _, r := range rows {
		out = append(out, r.AppID)
	}
	return out
}

func TestReleasesBetweenWalksBothListings(t *testing.T) {
	f := &fakeSearch{
		pageSize: 2,
		requests: map[string]int{},
		released: []fakeRow{
			{1, "Oct 4, 2026"}, // after the range: skipped
			{2, "Oct 1, 2026"},
			{3, "Sep 30, 2026"},
			{4, "Sep 29, 2026"},
			{5, "Sep 28, 2026"}, // before the range: ends the walk
			{6, "Sep 27, 2026"},
			{7, "Sep 26, 2026"},
			{8, "Sep 25, 2026"},
		},
		comingSoon: []fakeRow{
			{2, "Oct 1, 2026"}, // already listed as released: deduplicated
			{10, "Oct 1, 2026"},
			{11, "Oct 2, 2026"},
			{12, "October 2026"}, // imprecise: skipped
			{13, "Oct 4, 2026"},  // after the range: ends the walk
			{14, "Oct 5, 2026"},
			{15, "Oct 6, 2026"},
		},
	}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := NewClient(srv.URL)
	c.limiter = time.Tick(time.Millisecond)

	got, err := c.ReleasesBetween(t.Context(), ReleaseQuery{
		From: day("2026-09-29"), To: day("2026-10-03"), Today: day("2026-10-01"), IndieOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(appids(got)) != "[2 3 4 10 11]" {
		t.Errorf("appids = %v, want [2 3 4 10 11]", appids(got))
	}
	if got[0].ComingSoon || !got[3].ComingSoon {
		t.Errorf("ComingSoon flags wrong: %+v", got)
	}
	// Released: pages [1 2] [3 4] [5 ...] — the walk stops on page 3.
	// Coming soon: pages [2 10] [11 12] [13 ...] — stops on page 3.
	if f.requests["released"] != 3 || f.requests["comingsoon"] != 3 {
		t.Errorf("requests = %v, want 3 per listing", f.requests)
	}
	for _, tag := range f.tags {
		if tag != "492" {
			t.Errorf("tags = %q, want indie tag 492", tag)
		}
	}
}

func TestReleasesBetweenSkipsListingOutsideRange(t *testing.T) {
	f := &fakeSearch{pageSize: 10, requests: map[string]int{}, released: []fakeRow{{1, "Sep 1, 2026"}}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := NewClient(srv.URL)
	c.limiter = time.Tick(time.Millisecond)

	got, err := c.ReleasesBetween(t.Context(), ReleaseQuery{
		From: day("2026-09-01"), To: day("2026-09-30"), Today: day("2026-10-01"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || f.requests["comingsoon"] != 0 {
		t.Errorf("got %v, requests %v: a past range must not query coming soon", appids(got), f.requests)
	}
	if f.tags[0] != "" {
		t.Errorf("tags = %q, want none without IndieOnly", f.tags[0])
	}
	if _, err := c.ReleasesBetween(t.Context(), ReleaseQuery{From: day("2026-09-02"), To: day("2026-09-01")}); err == nil {
		t.Error("expected error for to before from")
	}
}
