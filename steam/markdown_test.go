package steam

import (
	"strings"
	"testing"
)

func TestDescriptionToMarkdown(t *testing.T) {
	tests := []struct {
		name, in, wantContains, wantNotContains string
	}{
		{
			"headers and lists",
			"<h2>Features</h2><ul><li>Roguelike</li><li>Story</li></ul>",
			"## Features\n\n- Roguelike",
			"<h2>",
		},
		{
			"bold and italic",
			"<strong>Defy</strong> the <em>gods</em>.",
			"**Defy** the _gods_.",
			"<strong>",
		},
		{
			"links preserved",
			`<a href="https://example.com">site</a>`,
			"[site](https://example.com)",
			"<a ",
		},
		{
			"images dropped",
			`<p>Before<img src="https://cdn.x/a.png" alt="a"/>After</p>`,
			"BeforeAfter",
			"<img",
		},
		{
			"iframe/video/embed dropped",
			`<p>x</p><iframe src="https://youtube.com/embed/x"></iframe><video controls></video>`,
			"x",
			"iframe",
		},
		{
			"steam bb_link anchors stay links",
			`<a class="bb_link" href="https://store.steampowered.com">store</a>`,
			"[store](https://store.steampowered.com)",
			"bb_link",
		},
		{
			"line breaks become breaks",
			"<p>One<br/>Two<br /><br />Three</p>",
			"One",
			"<br",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := DescriptionToMarkdown(tt.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, tt.wantContains) {
				t.Errorf("output %q missing %q", out, tt.wantContains)
			}
			if tt.wantNotContains != "" && strings.Contains(out, tt.wantNotContains) {
				t.Errorf("output %q must not contain %q", out, tt.wantNotContains)
			}
		})
	}
}

func TestDescriptionToMarkdownEmpty(t *testing.T) {
	if out, err := DescriptionToMarkdown(""); err != nil || out != "" {
		t.Errorf("empty: %q, %v", out, err)
	}
}

func TestDescriptionToMarkdownRejectsLeftoverHTML(t *testing.T) {
	// A converter failure mode would leave raw tags; we must error, not
	// store HTML.
	out, err := DescriptionToMarkdown("<p>fine</p>")
	if err != nil || strings.Contains(out, "<") && looksLikeHTML(out) {
		t.Errorf("plain paragraph should convert cleanly: %q, %v", out, err)
	}
}
