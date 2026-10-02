package steam

import (
	"regexp"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
)

// dropTagRe builds a non-backtracking (RE2-safe) pattern per tag name.
var dropTags = []string{"img", "iframe", "video", "audio", "embed", "object", "source", "script", "style"}

func dropTagRe(tag string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)<` + tag + `\b[^>]*(/>|>.*?</` + tag + `>)`)
}

var htmlToMD = md.NewConverter("", true, nil)

// DescriptionToMarkdown converts Steam's HTML description to Markdown for
// games.description_md. Images, videos and other embedded media are dropped
// (display media comes from the locally cached files), and the result is
// asserted HTML-free.
func DescriptionToMarkdown(html string) (string, error) {
	if strings.TrimSpace(html) == "" {
		return "", nil
	}
	cleaned := html
	for _, tag := range dropTags {
		cleaned = dropTagRe(tag).ReplaceAllString(cleaned, "")
	}

	out, err := htmlToMD.ConvertString(cleaned)
	if err != nil {
		return "", err
	}
	if strings.Contains(out, "<") && looksLikeHTML(out) {
		return "", errLeftoverHTML(out)
	}
	return strings.TrimSpace(out), nil
}

type leftoverHTMLError struct{ snippet string }

func (e *leftoverHTMLError) Error() string {
	return "converted description still looks like HTML: " + e.snippet
}

func errLeftoverHTML(s string) error { return &leftoverHTMLError{snippet: s[:min(len(s), 80)]} }

// looksLikeHTML reports whether s contains HTML tags (not lone '<' chars,
// which Markdown uses for e.g. "a < b").
func looksLikeHTML(s string) bool {
	return regexp.MustCompile(`(?i)</?[a-z][a-z0-9-]*(\s|>|/)`).MatchString(s)
}
