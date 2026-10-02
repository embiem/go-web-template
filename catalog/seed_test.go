package catalog

import (
	"strings"
	"testing"
)

func TestParseSeed(t *testing.T) {
	valid := `
developers:
  - slug: supergiant-games
    name: Supergiant Games
    website_url: https://www.supergiantgames.com
    bio: >-
      Known for Hades.
    socials: {x: "https://x.com/supergiantgames", bluesky: "https://bsky.app/profile/supergiantgames"}
games:
  - slug: hades
    title: Hades
    steam_appid: 1145360
    igdb_slug: hades--1
    release_date: 2020-09-17
    release_status: released
    developers: [supergiant-games]
    publishers: [supergiant-games]
    tags: [roguelike, action]
    tagline: "Escape the underworld."
    editorial_score: 95
    gem_note: >-
      A gem.
    store_links: {steam: "https://store.steampowered.com/app/1145360", gog: "https://www.gog.com/game/hades"}
`
	f, err := ParseSeed(strings.NewReader(valid))
	if err != nil {
		t.Fatalf("ParseSeed(valid) error: %v", err)
	}
	if len(f.Developers) != 1 || len(f.Games) != 1 {
		t.Fatalf("unexpected sizes: %+v", f)
	}
	g := f.Games[0]
	if g.SteamAppID == nil || *g.SteamAppID != 1145360 {
		t.Errorf("steam_appid not parsed: %v", g.SteamAppID)
	}
	if g.EditorialScore != 95 {
		t.Errorf("editorial_score = %d", g.EditorialScore)
	}
}

func TestParseSeedInvalid(t *testing.T) {
	tests := []struct {
		name, yaml, wantErr string
	}{
		{
			"unknown field",
			"games:\n  - slug: hades\ntitle: Hades\n",
			"field",
		},
		{
			"bad store",
			"games:\n  - slug: hades\n    title: Hades\n    release_status: released\n    editorial_score: 90\n    store_links: {origin: \"https://x\"}\n",
			"unknown store",
		},
		{
			"bad release_status",
			"games:\n  - slug: hades\n    title: Hades\n    release_status: out_now\n    editorial_score: 90\n",
			"release_status",
		},
		{
			"bad editorial score",
			"games:\n  - slug: hades\n    title: Hades\n    release_status: released\n    editorial_score: 101\n",
			"editorial_score",
		},
		{
			"bad date",
			"games:\n  - slug: hades\n    title: Hades\n    release_status: released\n    editorial_score: 90\n    release_date: 2020-9-17\n",
			"release_date",
		},
		{
			"unknown developer ref",
			"games:\n  - slug: hades\n    title: Hades\n    release_status: released\n    editorial_score: 90\n    developers: [nope]\n",
			"unknown developer",
		},
		{
			"uppercase slug",
			"games:\n  - slug: Hades\n    title: Hades\n    release_status: released\n    editorial_score: 90\n",
			"slug",
		},
		{
			"unknown social key",
			"developers:\n  - slug: sg\n    name: SG\n    socials: {twitter: \"https://x.com/sg\"}\n",
			"social",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSeed(strings.NewReader(tt.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestPickRotation(t *testing.T) {
	t.Run("avoids recent and no repeats", func(t *testing.T) {
		got := PickRotation([]string{"a", "b", "c", "d"}, map[string]bool{"b": true}, 3)
		want := []string{"a", "c", "d"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("got %v want %v", got, want)
		}
	})
	t.Run("wraps around after exhaustion", func(t *testing.T) {
		got := PickRotation([]string{"a", "b"}, map[string]bool{}, 4)
		want := []string{"a", "b", "a", "b"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("got %v want %v", got, want)
		}
	})
	t.Run("only recent candidates", func(t *testing.T) {
		got := PickRotation([]string{"a", "b"}, map[string]bool{"a": true, "b": true}, 2)
		want := []string{"a", "b"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("got %v want %v", got, want)
		}
	})
	t.Run("no candidates", func(t *testing.T) {
		if got := PickRotation(nil, nil, 3); len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
}

func TestParseDate(t *testing.T) {
	if _, m, d, err := ParseDate("2026-10-01"); err != nil || m != 10 || d != 1 {
		t.Errorf("ParseDate valid: %v %v %v", m, d, err)
	}
	for _, bad := range []string{"2026-13-01", "20261001", "2026-10-41", "x026-10-01"} {
		if _, _, _, err := ParseDate(bad); err == nil {
			t.Errorf("ParseDate(%q) expected error", bad)
		}
	}
}
