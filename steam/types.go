package steam

import (
	"strings"
	"time"
)

// AppDetails is the subset of appdetails we import.
// Steam omits fields (e.g. price_overview) entirely, hence pointers.
type AppDetails struct {
	Type                string   `json:"type"`
	Name                string   `json:"name"`
	SteamAppID          int64    `json:"steam_appid"`
	RequiredAge         int      `json:"required_age"`
	IsFree              bool     `json:"is_free"`
	ShortDescription    string   `json:"short_description"`
	DetailedDescription string   `json:"detailed_description"`
	AboutTheGame        string   `json:"about_the_game"`
	HeaderImage         string   `json:"header_image"`
	CapsuleImage        string   `json:"capsule_image"`
	CapsuleImageV5      string   `json:"capsule_imagev5"`
	BackgroundRaw       string   `json:"background_raw"`
	Website             string   `json:"website"`
	Developers          []string `json:"developers"`
	Publishers          []string `json:"publishers"`
	ReleaseDate         struct {
		ComingSoon bool   `json:"coming_soon"`
		Date       string `json:"date"`
	} `json:"release_date"`
	Screenshots []struct {
		ID            int    `json:"id"`
		PathThumbnail string `json:"path_thumbnail"`
		PathFull      string `json:"path_full"`
	} `json:"screenshots"`
	Genres []struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	} `json:"genres"`
	Categories []struct {
		ID          int    `json:"id"`
		Description string `json:"description"`
	} `json:"categories"`
}

// ReviewSummary is the appreviews query_summary aggregate.
type ReviewSummary struct {
	ReviewScore     int    `json:"review_score"` // 0-9, Steam's descending scale
	ReviewScoreDesc string `json:"review_score_desc"`
	TotalPositive   int64  `json:"total_positive"`
	TotalNegative   int64  `json:"total_negative"`
	TotalReviews    int64  `json:"total_reviews"`
}

// ReviewPct returns total_positive/total_reviews as a 0-100 percentage, or
// 0 when there are no reviews.
func (s *ReviewSummary) ReviewPct() float64 {
	if s == nil || s.TotalReviews <= 0 {
		return 0
	}
	return float64(s.TotalPositive) / float64(s.TotalReviews) * 100
}

// steamSentinelDate is what the API returns for coming-soon games without a
// real date.
const steamSentinelDate = "Dec 31, 9998"

// ParseReleaseDate parses Steam's free-text release_date.date. Verified
// formats first, then community-seen formats; anything unparseable (and the
// "Dec 31, 9998" sentinel) yields the zero time and ok=false.
func ParseReleaseDate(date string, comingSoon bool) (time.Time, bool) {
	date = strings.TrimSpace(date)
	if date == "" || date == steamSentinelDate {
		return time.Time{}, false
	}

	// "Sep 17, 2020" and "17 Sep, 2020" are the verified store formats.
	layouts := []string{
		"Jan 2, 2006",
		"2 Jan, 2006",
		"Jan 2006", // "Sep 2020" — month precision
		"2006",     // year only
		"January 2, 2006",
		"2 January 2006",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, date); err == nil {
			return t, true
		}
	}
	// "Q1 2026", "Coming soon", "TBA", typos -> unknown.
	return time.Time{}, false
}

// ReleaseStatus maps an appdetails entry to the catalog release_status values.
func (d *AppDetails) ReleaseStatus() string {
	if d.ReleaseDate.ComingSoon {
		return "upcoming"
	}
	return "released"
}
