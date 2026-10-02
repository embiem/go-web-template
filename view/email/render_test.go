package email

import (
	"strings"
	"testing"
)

func fixtureDoc() Doc {
	return Doc{
		Subject:   "Weekly gems",
		Preheader: "The good stuff first",
		Blocks: []Block{
			{ProseHTML: `<p style="margin:0 0 14px 0;">Hello <strong>friends</strong>.</p>`, ProseText: "Hello friends."},
			{Gotd: &Gotd{
				Heading:  "Game of the Day — September 28",
				NoteHTML: `<p style="margin:0 0 14px 0;">Still unbeatable.</p>`,
				NoteText: "Still unbeatable.",
				Game: GameCard{
					Title: "Hollow Knight", Tagline: "A metroidvania classic.",
					Developer: "Team Cherry", GameURL: "https://gems.test/games/hollow-knight",
					ImageURL:    "https://gems.test/media/games/hollow-knight/header.jpg",
					ReleaseLine: "Released Feb 24, 2017", ScoreLine: "Gem score 92",
					Stores: []StoreButton{
						{Label: "Steam", URL: "https://store.steampowered.com/app/367520"},
						{Label: "GOG", URL: "https://www.gog.com/game/hollow_knight"},
					},
				},
			}},
			{Radar: &Section{Heading: "Release Radar — 2026-W40", Items: []GameCard{
				{Title: "Silksong", GameURL: "https://gems.test/games/silksong", ReleaseLine: "Released Sep 29, 2026", Developer: "Team Cherry"},
				{Title: "Ball x Pit", GameURL: "https://gems.test/games/ball-x-pit", ReleaseLine: "Released Oct 1, 2026"},
			}}},
			{Top: &Section{Heading: "Top of the Month — September 2026", Items: []GameCard{
				{Title: "Hades", GameURL: "https://gems.test/games/hades", ScoreLine: "Gem score 93"},
			}}},
			{Game: &GameCard{Title: "Stardew Valley", GameURL: "https://gems.test/games/stardew-valley",
				Stores: []StoreButton{{Label: "Website", URL: "https://stardewvalley.net"}}}},
		},
		Footer: Footer{
			HomeURL: "https://gems.test", BrowserURL: "https://gems.test/newsletter/issues/2026-w40",
			UnsubURL: "{{unsubscribe_url}}", PostalAddress: "1 Gem Way, Gemtown",
			Reason: "You subscribed at https://gems.test/newsletter.",
		},
	}
}

// The plain-text part is what delivers deliverability: it must carry every
// game link and the unsubscribe URL in full.
func TestTextContainsEveryGameLink(t *testing.T) {
	text := Text(fixtureDoc())
	for _, want := range []string{
		"https://gems.test/games/hollow-knight",
		"https://gems.test/games/silksong",
		"https://gems.test/games/ball-x-pit",
		"https://gems.test/games/hades",
		"https://gems.test/games/stardew-valley",
		"https://store.steampowered.com/app/367520",
		"https://www.gog.com/game/hollow_knight",
		"https://stardewvalley.net",
		"https://gems.test/newsletter/issues/2026-w40",
		"Unsubscribe: {{unsubscribe_url}}",
		"1 Gem Way, Gemtown",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q\n---\n%s", want, text)
		}
	}
	if strings.Contains(text, "<") && strings.Contains(text, "div") {
		t.Fatalf("text contains raw HTML:\n%s", text)
	}
	t.Log("\n" + text)
}

func TestRenderHTML(t *testing.T) {
	doc := fixtureDoc()
	html, err := RenderHTML(doc)
	if err != nil {
		t.Fatalf("RenderHTML: %v", err)
	}
	for _, want := range []string{
		"<!doctype html>", `color-scheme`, doc.Subject, doc.Preheader,
		"Game of the Day", "Hollow Knight", "https://gems.test/media/games/hollow-knight/header.jpg",
		"Release Radar", "Top of the Month", "Stardew Valley",
		`style="`, "&#9670;", // inline styles present; wordmark
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("html missing %q", want)
		}
	}
	for _, banned := range []string{"<script", "<form", "<style"} {
		if strings.Contains(html, banned) {
			t.Fatalf("html contains %s", banned)
		}
	}
	// Blocked images must not break the mail: alt text everywhere.
	if n := strings.Count(html, "alt="); n == 0 {
		t.Fatal("no image alt texts")
	}
}

func TestRenderHTMLValidation(t *testing.T) {
	if _, err := RenderHTML(Doc{Subject: ""}); err == nil {
		t.Fatal("empty subject accepted")
	}
	bad := Doc{Subject: "s", Blocks: []Block{{ProseHTML: "x", Game: &GameCard{}}}}
	if _, err := RenderHTML(bad); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("multi-field block accepted: %v", err)
	}
}

func TestProseInlinesStyles(t *testing.T) {
	md := "Hello **world**, [a link](https://example.com).\n\n- one\n- two\n\n> quoted\n\n---"
	html := Prose(md)
	for _, want := range []string{
		`<p style=`, `<strong style=`, `<a style="color:#8b3fe6;text-decoration:underline;" href="https://example.com"`,
		`<ul style=`, `<li style=`, `<blockquote style=`, `<hr style=`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("prose missing %q in:\n%s", want, html)
		}
	}
	if Prose("") != "" {
		t.Fatal("empty prose not empty")
	}
}

func TestProseTextStripsMarkdown(t *testing.T) {
	in := "## Heading\n\nA [link](https://example.com) and **bold** and *soft* and `code`.\n\n![alt](https://img)\n"
	out := ProseText(in)
	for _, want := range []string{
		"Heading", "A link (https://example.com) and bold and soft and code.",
		"[image: alt]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("text missing %q in:\n%s", want, out)
		}
	}
	for _, banned := range []string{"**", "](http", "<"} {
		if strings.Contains(out, banned) {
			t.Fatalf("text contains %q:\n%s", banned, out)
		}
	}
}

func TestRenderConfirm(t *testing.T) {
	doc := ConfirmDoc{ConfirmURL: "https://gems.test/newsletter/confirm?token=abc", HomeURL: "https://gems.test", PostalAddress: "1 Gem Way"}
	html, err := RenderConfirmHTML(doc)
	if err != nil {
		t.Fatalf("RenderConfirmHTML: %v", err)
	}
	if !strings.Contains(html, "confirm?token=abc") || strings.Contains(html, "unsubscribe") {
		t.Fatalf("confirm html wrong:\n%s", html)
	}
	if text := ConfirmText(doc); !strings.Contains(text, "Confirm: https://gems.test/newsletter/confirm?token=abc") {
		t.Fatalf("confirm text wrong:\n%s", text)
	}
	if _, err := RenderConfirmHTML(ConfirmDoc{}); err == nil {
		t.Fatal("empty ConfirmURL accepted")
	}
}
