package util

import (
	"strings"
	"testing"
)

func TestMdToHTML(t *testing.T) {
	// Third-party game descriptions (Steam/IGDB) must never smuggle raw HTML
	// into the page: skip it entirely, not escaped-but-rendered.
	forbids := []struct {
		name     string
		input    string
		forbiden []string
	}{
		{name: "script tag", input: "hello <script>alert(1)</script>", forbiden: []string{"<script", "onerror"}},
		{name: "img onerror", input: "<img src=x onerror=alert(1)>", forbiden: []string{"<img", "onerror"}},
		{name: "inline html", input: "keep <b>bold</b> out", forbiden: []string{"<b>"}},
		{name: "html block", input: "<div onclick=\"x()\">gone</div>\n\nkept", forbiden: []string{"<div", "onclick"}},
		{name: "event handler attr", input: "<a href=\"#\" onclick=\"evil()\">x</a>", forbiden: []string{"onclick"}},
	}
	for _, tc := range forbids {
		t.Run(tc.name, func(t *testing.T) {
			got := MdToHTML(tc.input)
			for _, forbidden := range tc.forbiden {
				if strings.Contains(got, forbidden) {
					t.Errorf("MdToHTML(%q) = %q, must not contain %q", tc.input, got, forbidden)
				}
			}
		})
	}
}

func TestDescriptionPreview(t *testing.T) {
	// Paragraphs are kept, everything else goes to rest.
	long := "P1a\n\nP2\n\nP3\n\nP4\n\nP5"
	intro, rest := DescriptionPreview(long, 3)
	if intro != "P1a\n\nP2\n\nP3" || rest != "P4\n\nP5" {
		t.Errorf("DescriptionPreview = %q, %q", intro, rest)
	}
	// Leading headings (Steam DLC/update promos) are skipped for the preview
	// but preserved in rest so no content is dropped.
	promo := "# Hollow Knight Expands with Free Content\n\nGodmaster - Take your place among the Gods.\n\nLifeblood - A Kingdom Upgraded!"
	intro, rest = DescriptionPreview(promo, 2)
	if intro != "Godmaster - Take your place among the Gods.\n\nLifeblood - A Kingdom Upgraded!" {
		t.Errorf("promo intro = %q", intro)
	}
	if rest != "# Hollow Knight Expands with Free Content" {
		t.Errorf("promo rest = %q", rest)
	}
	// Nothing to hide when the text has at most keepParas blocks.
	intro, rest = DescriptionPreview("P1\n\nP2", 3)
	if intro != "P1\n\nP2" || rest != "" {
		t.Errorf("short split = %q, %q; want full text and empty rest", intro, rest)
	}
	intro, rest = DescriptionPreview("", 3)
	if intro != "" || rest != "" {
		t.Errorf("empty split = %q, %q", intro, rest)
	}
	// Heading-only descriptions stay intact.
	intro, rest = DescriptionPreview("# Only a heading", 2)
	if intro != "# Only a heading" || rest != "" {
		t.Errorf("heading-only split = %q, %q", intro, rest)
	}
}

func TestMdToHTMLContains(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{name: "heading", input: "# Hi", contains: "<h1"},
		{name: "paragraph", input: "hello world", contains: "<p"},
		{name: "emphasis", input: "*hi*", contains: "<em"},
		{name: "link", input: "[gog](https://www.gog.com)", contains: `<a href="https://www.gog.com"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MdToHTML(tc.input)
			if !strings.Contains(got, tc.contains) {
				t.Errorf("MdToHTML(%q) = %q, want it to contain %q", tc.input, got, tc.contains)
			}
		})
	}
}

// Safelink keeps dangerous schemes out of rendered hrefs. This also protects
// the RSS feed, which has no CSP.
func TestMdToHTMLSafelink(t *testing.T) {
	unsafe := []string{
		"[x](javascript:alert(1))",
		"[x](JAVASCRIPT:alert(1))",
		"[x](data:text/html,<script>alert(1)</script>)",
		"[x](vbscript:msgbox(1))",
	}
	for _, in := range unsafe {
		if got := MdToHTML(in); strings.Contains(got, "javascript:") || strings.Contains(got, "vbscript:") ||
			strings.Contains(got, "data:text/html") {
			t.Errorf("MdToHTML(%q) = %q, dangerous href survived", in, got)
		}
	}
	safe := []struct{ name, input string }{
		{"https", "[x](https://example.com)"},
		{"http", "[x](http://example.com)"},
		{"mailto", "[x](mailto:hi@example.com)"},
		{"relative", "[x](/games/hades)"},
	}
	for _, tc := range safe {
		if got := MdToHTML(tc.input); !strings.Contains(got, `href="`) {
			t.Errorf("MdToHTML(%q) = %q, safe link lost its href", tc.input, got)
		}
	}
}

// Newlines are preserved: no SSE here any more, and stripping them glued
// soft-wrapped words together.
func TestMdToHTMLKeepsNewlines(t *testing.T) {
	got := MdToHTML("a soft\nwrapped line")
	if !strings.Contains(got, "soft\nwrapped") {
		t.Errorf("MdToHTML = %q, want the soft line break preserved", got)
	}
}
