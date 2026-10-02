package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Seed file format (data/seed/games.yaml, written by curators).
type SeedFile struct {
	Developers []SeedDeveloper `yaml:"developers"`
	Games      []SeedGame      `yaml:"games"`
}

type SeedDeveloper struct {
	Slug       string            `yaml:"slug"`
	Name       string            `yaml:"name"`
	WebsiteURL string            `yaml:"website_url"`
	Bio        string            `yaml:"bio"`
	Socials    map[string]string `yaml:"socials"`
}

type SeedGame struct {
	Slug           string            `yaml:"slug"`
	Title          string            `yaml:"title"`
	SteamAppID     *int64            `yaml:"steam_appid"`
	IGDBSlug       string            `yaml:"igdb_slug"`
	ReleaseDate    string            `yaml:"release_date"` // YYYY-MM-DD or empty
	ReleaseStatus  string            `yaml:"release_status"`
	Developers     []string          `yaml:"developers"`
	Publishers     []string          `yaml:"publishers"`
	Tags           []string          `yaml:"tags"`
	Tagline        string            `yaml:"tagline"`
	Description    string            `yaml:"description"`
	EditorialScore int               `yaml:"editorial_score"`
	GemNote        string            `yaml:"gem_note"`
	WebsiteURL     string            `yaml:"website_url"`
	StoreLinks     map[string]string `yaml:"store_links"`
}

// Valid values for constrained fields; the DB enforces the same via CHECK
// constraints (see db/migrations).
var (
	ValidReleaseStatuses = map[string]bool{
		"announced": true, "upcoming": true, "early_access": true, "released": true,
	}
	ValidStores = map[string]bool{
		"steam": true, "gog": true, "epic": true, "itch": true, "humble": true, "direct": true,
	}
	ValidSocials = map[string]bool{
		"x": true, "bluesky": true, "mastodon": true, "youtube": true,
		"discord": true, "twitch": true, "instagram": true,
	}
)

// ParseSeedFile opens path and parses + validates the seed file.
func ParseSeedFile(path string) (*SeedFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseSeed(f)
}

// MarshalSocials encodes developer social profiles as JSONB, rejecting
// unknown keys and empty values.
func MarshalSocials(socials map[string]string) ([]byte, error) {
	if len(socials) == 0 {
		return []byte("{}"), nil
	}
	for k, v := range socials {
		if !ValidSocials[k] {
			return nil, fmt.Errorf("unknown social key %q", k)
		}
		if v == "" {
			return nil, fmt.Errorf("social %q has an empty URL", k)
		}
	}
	return json.Marshal(socials)
}

// ParseSeed reads a seed YAML file and validates it. Validation is purely
// structural (no DB access) so it stays unit-testable.
func ParseSeed(r io.Reader) (*SeedFile, error) {
	var f SeedFile
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true) // reject unknown fields — catches curator typos
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("parsing seed YAML: %w", err)
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// Validate checks slugs, statuses, scores, store names and social keys, and
// that every referenced developer slug exists in the file.
func (f *SeedFile) Validate() error {
	devSlugs := map[string]bool{}
	for i, d := range f.Developers {
		if err := validSlug(d.Slug); err != nil {
			return fmt.Errorf("developers[%d]: %w", i, err)
		}
		if d.Name == "" {
			return fmt.Errorf("developers[%d] (%s): name is required", i, d.Slug)
		}
		for k := range d.Socials {
			if !ValidSocials[k] {
				return fmt.Errorf("developers[%d] (%s): unknown social key %q (allowed: x, bluesky, mastodon, youtube, discord, twitch, instagram)", i, d.Slug, k)
			}
		}
		if devSlugs[d.Slug] {
			return fmt.Errorf("developers[%d]: duplicate slug %q", i, d.Slug)
		}
		devSlugs[d.Slug] = true
	}

	gameSlugs := map[string]bool{}
	for i, g := range f.Games {
		if err := validSlug(g.Slug); err != nil {
			return fmt.Errorf("games[%d]: %w", i, err)
		}
		if gameSlugs[g.Slug] {
			return fmt.Errorf("games[%d]: duplicate slug %q", i, g.Slug)
		}
		gameSlugs[g.Slug] = true
		if g.Title == "" {
			return fmt.Errorf("games[%d] (%s): title is required", i, g.Slug)
		}
		if g.ReleaseDate != "" {
			if err := validDate(g.ReleaseDate); err != nil {
				return fmt.Errorf("games[%d] (%s): release_date %w", i, g.Slug, err)
			}
		}
		if !ValidReleaseStatuses[g.ReleaseStatus] {
			return fmt.Errorf("games[%d] (%s): invalid release_status %q (allowed: announced, upcoming, early_access, released)", i, g.Slug, g.ReleaseStatus)
		}
		if g.EditorialScore < 0 || g.EditorialScore > 100 {
			return fmt.Errorf("games[%d] (%s): editorial_score %d out of range 0-100", i, g.Slug, g.EditorialScore)
		}
		for _, ref := range append(append([]string{}, g.Developers...), g.Publishers...) {
			if !devSlugs[ref] {
				return fmt.Errorf("games[%d] (%s): references unknown developer slug %q", i, g.Slug, ref)
			}
		}
		for store := range g.StoreLinks {
			if !ValidStores[store] {
				return fmt.Errorf("games[%d] (%s): unknown store %q (allowed: steam, gog, epic, itch, humble, direct)", i, g.Slug, store)
			}
		}
		for _, tag := range g.Tags {
			if err := validSlug(tag); err != nil {
				return fmt.Errorf("games[%d] (%s): tag %w", i, g.Slug, err)
			}
		}
	}
	return nil
}

func validSlug(s string) error {
	if s == "" {
		return fmt.Errorf("slug is required")
	}
	if strings.ToLower(s) != s || strings.ContainsAny(s, " /\\") {
		return fmt.Errorf("invalid slug %q (want lowercase, hyphens, no spaces)", s)
	}
	return nil
}

func validDate(s string) error {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return fmt.Errorf("must be YYYY-MM-DD")
	}
	var y, m, d int
	if _, err := fmt.Sscanf(s, "%d-%d-%d", &y, &m, &d); err != nil {
		return fmt.Errorf("must be YYYY-MM-DD")
	}
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return fmt.Errorf("month/day out of range")
	}
	return nil
}
