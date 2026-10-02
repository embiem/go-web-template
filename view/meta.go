package view

import (
	"os"
	"strings"
)

// PageMeta carries per-page SEO/social metadata into Layout.
type PageMeta struct {
	// Title is the page-specific part; Layout appends the site name
	// (".TitleSuffix") unless Title already contains it (home page).
	Title       string
	Description string
	// Canonical is the request path, e.g. "/games/hades"; Layout makes it
	// absolute using BASE_URL.
	Canonical string
	// OGImage is an absolute URL (usually a hero/header image); optional.
	OGImage string
	// OGType defaults to "website"; game pages use "website" too (product is
	// poorly supported by scrapers for games).
	OGType string
	// NoIndex keeps archive/parametrised pages out of search results.
	NoIndex bool
}

// TitleSuffix is appended to every page title by Layout.
const TitleSuffix = "Indie Game Gems"

// BaseURL returns the site's public origin (BASE_URL, default
// http://localhost:3000) without a trailing slash.
func BaseURL() string {
	base := strings.TrimRight(os.Getenv("BASE_URL"), "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	return base
}

// AbsURL turns a site path into an absolute URL (feeds, sitemaps, og:url).
func AbsURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return BaseURL() + path
}

// FullTitle renders meta.Title + suffix, or the default when unset.
func (m PageMeta) FullTitle() string {
	if m.Title == "" {
		return TitleSuffix + " — discover great indie games you missed"
	}
	if strings.Contains(m.Title, TitleSuffix) {
		return m.Title
	}
	return m.Title + " · " + TitleSuffix
}

// ogType defaults to website.
func (m PageMeta) ogType() string {
	if m.OGType == "" {
		return "website"
	}
	return m.OGType
}

// ogDescription defaults to the site tagline.
func (m PageMeta) ogDescription() string {
	if m.Description == "" {
		return "Discover great indie games you might have missed — curated gems, daily picks, release radar and monthly tops, outside the Steam algorithm."
	}
	return m.Description
}
