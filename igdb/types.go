package igdb

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Enum IDs from the official IGDB docs (2026: enum fields migrated to tables,
// names as documented in tmp/research/igdb.md).

// ExternalGameSource: external_games.external_game_source ids we use.
const (
	SourceSteam = 1
	SourceGOG   = 5
	SourceEpic  = 26
	SourceItch  = 30
)

// WebsiteType: websites/company_websites.type ids we use.
const (
	WebsiteOfficial  = 1
	WebsiteSteam     = 13
	WebsiteItch      = 15
	WebsiteEpicGames = 16
	WebsiteGOG       = 17
)

// GameType ids for catalogue filtering.
const (
	GameTypeMainGame = 0
	GameTypeDLC      = 1
	GameTypeBundle   = 3
)

// GameStatus ids.
const (
	GameStatusReleased    = 0
	GameStatusEarlyAccess = 4
	GameStatusCancelled   = 6
)

// Game is the IGDB games payload we request (subset; fields are pointers
// because IGDB omits unset fields).
type Game struct {
	ID                int64             `json:"id"`
	Name              string            `json:"name"`
	Slug              string            `json:"slug"`
	Summary           *string           `json:"summary"`
	FirstReleaseDate  *int64            `json:"first_release_date"` // Unix seconds
	GameType          *int              `json:"game_type"`
	GameStatus        *int              `json:"game_status"`
	Cover             *Image            `json:"cover"`
	Screenshots       []Image           `json:"screenshots"`
	Videos            []Video           `json:"videos"`
	Websites          []Website         `json:"websites"`
	InvolvedCompanies []InvolvedCompany `json:"involved_companies"`
	ExternalGames     []ExternalGame    `json:"external_games"`
}

// ReleaseDate converts first_release_date (Unix seconds) to UTC.
func (g Game) ReleaseDate() *time.Time {
	if g.FirstReleaseDate == nil {
		return nil
	}
	t := time.Unix(*g.FirstReleaseDate, 0).UTC()
	return &t
}

// YouTubeTrailer picks the first video named like a trailer, else the first
// video. Returns "" when the game has no videos.
func (g Game) YouTubeTrailer() string {
	for _, v := range g.Videos {
		if strings.Contains(strings.ToLower(v.Name), "trailer") && v.VideoID != "" {
			return v.VideoID
		}
	}
	if len(g.Videos) > 0 {
		return g.Videos[0].VideoID
	}
	return ""
}

// StoreLinks maps IGDB websites/external_games to our store names (no
// "direct": the official website belongs in games.website_url instead).
func (g Game) StoreLinks() map[string]string {
	links := map[string]string{}
	for _, eg := range g.ExternalGames {
		switch eg.ExternalGameSource {
		case SourceSteam:
			links["steam"] = eg.URL
		case SourceGOG:
			links["gog"] = eg.URL
		case SourceEpic:
			links["epic"] = eg.URL
		case SourceItch:
			links["itch"] = eg.URL
		}
	}
	for _, w := range g.Websites {
		switch w.Type {
		case WebsiteSteam:
			links["steam"] = firstNonEmpty(w.URL, links["steam"])
		case WebsiteGOG:
			links["gog"] = firstNonEmpty(w.URL, links["gog"])
		case WebsiteEpicGames:
			links["epic"] = firstNonEmpty(w.URL, links["epic"])
		case WebsiteItch:
			links["itch"] = firstNonEmpty(w.URL, links["itch"])
		}
	}
	return links
}

// OfficialWebsite returns the type=1 (official) website URL, if any.
func (g Game) OfficialWebsite() string {
	for _, w := range g.Websites {
		if w.Type == WebsiteOfficial {
			return w.URL
		}
	}
	return ""
}

// InvolvedCompany is an expanded involved_companies entry.
type InvolvedCompany struct {
	Company   Company `json:"company"`
	Developer bool    `json:"developer"`
	Publisher bool    `json:"publisher"`
}

// Company is an expanded companies entry.
type Company struct {
	ID          int64            `json:"id"`
	Name        string           `json:"name"`
	Slug        string           `json:"slug"`
	Description *string          `json:"description"`
	Websites    []CompanyWebsite `json:"websites"`
	Logo        *Image           `json:"logo"`
}

// Website is an expanded game website.
type Website struct {
	ID   int64  `json:"id"`
	URL  string `json:"url"`
	Type int    `json:"type"`
}

// CompanyWebsite is an expanded company website.
type CompanyWebsite struct {
	URL  string `json:"url"`
	Type int    `json:"type"`
}

// ExternalGame is an expanded external_games entry.
type ExternalGame struct {
	ID                 int64  `json:"id"`
	UID                string `json:"uid"` // Steam appid as a string for source 1
	ExternalGameSource int    `json:"external_game_source"`
	URL                string `json:"url"`
	Game               int64  `json:"game"`
}

// Video is an expanded game_videos entry (YouTube).
type Video struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	VideoID string `json:"video_id"`
}

// Image is an IGDB image reference; ImageURL builds the CDN URL at a size.
type Image struct {
	ImageID string `json:"image_id"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// Image size tokens (official docs).
const (
	SizeCoverBig      = "cover_big"      // 264x374 fit
	SizeScreenshotBig = "screenshot_big" // 889x500 lfill
	SizeLogoMed       = "logo_med"       // 284x160 fit
	Size720p          = "720p"           // 1280x720 fit
)

// ImageURL builds the CDN URL for an image at the given size token.
// Note: IGDB deletes replaced images from the CDN after 30 days — our media
// cache should re-fetch via sync, not assume permanence.
func ImageURL(imageID, size string) string {
	return fmt.Sprintf("https://images.igdb.com/igdb/image/upload/t_%s/%s.jpg", size, imageID)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// SteamUID renders an appid as IGDB's string uid.
func SteamUID(appid int64) string { return strconv.FormatInt(appid, 10) }
