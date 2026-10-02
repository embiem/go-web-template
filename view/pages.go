package view

import (
	"time"

	"github.com/embiem/indie-game-gems/data"
)

// View models for the public pages. Handlers fill these; the templ files only
// shape markup. Dates are UTC everywhere.

// PickFeature is a Game of the Day presentation (card + editorial note).
type PickFeature struct {
	Card     data.GameCard
	PickDate time.Time
	NoteMd   string
	Stores   []StoreButton
}

// TopSection is a "Top of the Month" slice with its (possibly previous-month)
// label, so the home section can honestly label what it shows.
type TopSection struct {
	Label string // "September 2026"
	Href  string // "/top/2026/09"
	Cards []data.GameCard
}

// RadarSection is this week's release row on the home page. When the week is
// thin, exactly one filler group is set: Upcoming ("Coming up") or Recent
// ("Recently released").
type RadarSection struct {
	Label    string // "2026-W40"
	Range    string // "Sep 28 – Oct 4"
	Cards    []data.GameCard
	Upcoming []data.GameCard
	Recent   []data.GameCard
}

type HomePageData struct {
	Meta  PageMeta
	Pick  *PickFeature // nil → friendly empty state
	Radar RadarSection
	Top   TopSection
	Gems  []data.GameCard
}

// PickDay groups radar games by their release day.
type PickDay struct {
	Date  time.Time
	Cards []data.GameCard
}

type PickPageData struct {
	Meta    PageMeta
	Pick    PickFeature
	Prev    string // href, empty = no navigation
	Next    string
	Archive []data.ListRecentPicksRow
}

type RadarPageData struct {
	Meta  PageMeta
	Label string // "2026-W40"
	Range string // "Sep 28 – Oct 4"
	Prev  PagerLink
	Next  PagerLink
	Days  []PickDay
}

type TopPageData struct {
	Meta  PageMeta
	Label string // "September 2026"
	Prev  PagerLink
	Next  PagerLink
	Cards []data.GameCard
}

type GamePageData struct {
	Meta        PageMeta
	Game        data.Game
	Developers  []data.ListGameDevelopersRow
	Publishers  []data.ListGameDevelopersRow
	Stores      []StoreButton
	HeroURL     string // resolved hero/header image URL, "" → gradient hero
	Screenshots []data.Medium
	TrailerID   string // YouTube id, "" hides the trailer section
	TrailerURL  string // poster image URL for the click-to-load facade
	Tags        []data.Tag
	MoreFrom    []data.GameCard
	MoreFromDev string // developer name for "More from {name}"
	JSONLD      map[string]any
}

type DeveloperPageData struct {
	Meta      PageMeta
	Developer data.Developer
	Socials   []SocialLink
	Games     []data.GameCard
}
