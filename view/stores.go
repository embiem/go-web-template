package view

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/embiem/indie-game-gems/data"
	"github.com/jackc/pgx/v5/pgtype"
)

// Store button presentation: labels and ordering live here so they're unit
// testable, separate from the templ markup in components.templ.

// StoreLabel is the button text for one store, depending on the game's
// release status. Steam is the primary store: announced/upcoming games get
// "Wishlist on Steam" (a plain link to the store page — there is no wishlist
// API), released/early access games get "Buy on Steam".
type StoreButton struct {
	Store string // steam | gog | epic | itch | humble | direct
	Label string
	URL   string
	// Primary styles the button as the big call-to-action (Steam only).
	Primary bool
}

// storeOrder defines the display order: Steam first, then the others.
var storeOrder = []string{"steam", "gog", "epic", "itch", "humble", "direct"}

// storeLabels maps store -> static label; Steam gets a status-dependent label.
var storeLabels = map[string]string{
	"gog":    "GOG",
	"epic":   "Epic Games Store",
	"itch":   "itch.io",
	"humble": "Humble",
	"direct": "Developer's store",
}

// steamLabel picks the Steam call-to-action for a release status.
func steamLabel(releaseStatus string) string {
	switch releaseStatus {
	case "announced", "upcoming":
		return "Wishlist on Steam"
	default: // released, early_access — and anything unknown stays buyable
		return "Buy on Steam"
	}
}

// StoreButtons turns store link rows into ordered, labelled buttons.
// Steam is always first and primary; unknown store kinds are dropped (the
// CHECK constraint keeps them out, this is belt-and-braces for partial data).
func StoreButtons(links []data.StoreLink, releaseStatus string) []StoreButton {
	urls := make(map[string]string, len(links))
	for _, l := range links {
		if l.Url != "" {
			urls[l.Store] = l.Url
		}
	}
	buttons := make([]StoreButton, 0, len(urls))
	for _, store := range storeOrder {
		url, ok := urls[store]
		if !ok {
			continue
		}
		label := storeLabels[store]
		if store == "steam" {
			label = steamLabel(releaseStatus)
		}
		buttons = append(buttons, StoreButton{Store: store, Label: label, URL: url, Primary: store == "steam"})
	}
	return buttons
}

// ReleaseBadgeText renders the release status badge label, e.g.
// "Out now · Feb 24, 2017" or "Wishlist · Feb 2027" for future dates.
func ReleaseBadgeText(status string, date pgtype.Date) string {
	switch status {
	case "announced":
		return "Announced" + releaseDateSuffix(date)
	case "upcoming":
		return "Upcoming" + releaseDateSuffix(date)
	case "early_access":
		return "Early access" + releaseDateSuffix(date)
	default: // released
		return "Out now" + releaseDateSuffix(date)
	}
}

// releaseDateSuffix formats known dates ("· Sep 17, 2020"); year-only for
// anything further ahead than 12 months to avoid false precision on far-out
// announced dates.
func releaseDateSuffix(date pgtype.Date) string {
	if !date.Valid {
		return ""
	}
	return " · " + FormatReleaseDate(date.Time)
}

// FormatReleaseDate renders dates in a stable, compact form.
func FormatReleaseDate(t time.Time) string {
	return t.Format("Jan 2, 2006")
}

// GemScoreLabel renders a rounded score ("93") or "" when unscored.
func GemScoreLabel(score pgtype.Float4) string {
	if !score.Valid {
		return ""
	}
	return fmt.Sprintf("%.0f", score.Float32)
}

// SocialLink is one developer social profile, resolved from the allowed
// socials JSONB keys.
type SocialLink struct {
	Label string
	URL   string
}

// socialLabels maps allowed JSONB keys to display labels.
var socialLabels = map[string]string{
	"x":         "X",
	"bluesky":   "Bluesky",
	"mastodon":  "Mastodon",
	"youtube":   "YouTube",
	"discord":   "Discord",
	"twitch":    "Twitch",
	"instagram": "Instagram",
}

// DeveloperSocials parses the socials JSONB into ordered links; unknown keys
// are ignored (the schema documents the allowed set).
func DeveloperSocials(socialsJSON []byte) []SocialLink {
	raw := map[string]string{}
	if len(socialsJSON) > 0 {
		if err := json.Unmarshal(socialsJSON, &raw); err != nil {
			return nil
		}
	}
	links := make([]SocialLink, 0, len(raw))
	for _, key := range []string{"x", "bluesky", "mastodon", "youtube", "discord", "twitch", "instagram"} {
		if url := strings.TrimSpace(raw[key]); url != "" {
			links = append(links, SocialLink{Label: socialLabels[key], URL: url})
		}
	}
	return links
}
