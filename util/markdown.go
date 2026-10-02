/*
Package util contains utility functions.
Like markdown rendering & logging.
*/
package util

import (
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

func MdToHTML(md string) string {
	// create markdown parser with extensions
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock
	p := parser.NewWithExtensions(extensions)
	doc := p.Parse([]byte(md))

	// create HTML renderer with extensions. SkipHTML drops raw HTML
	// blocks/inline tags completely: descriptions are third-party content
	// (Steam/IGDB), so no raw HTML may pass through into the page. Safelink
	// restricts link targets to http(s)/mailto/relative — no javascript: or
	// data: hrefs from third-party markdown (also protects the RSS feed,
	// which has no CSP).
	htmlFlags := html.CommonFlags | html.HrefTargetBlank | html.SkipHTML |
		html.Safelink | html.NoopenerLinks | html.NoreferrerLinks
	opts := html.RendererOptions{Flags: htmlFlags}
	renderer := html.NewRenderer(opts)

	return string(markdown.Render(doc, renderer))
}

// DescriptionPreview splits third-party markdown at a paragraph boundary
// ("\n\n") for the game page's collapsed description preview. Steam
// descriptions often open with DLC/update heading promos, so leading headings
// are skipped and up to keepParas paragraphs are shown instead. Headings that
// were skipped stay at the front of `rest` so nothing is dropped.
func DescriptionPreview(md string, keepParas int) (intro, rest string) {
	blocks := strings.Split(strings.TrimSpace(md), "\n\n")
	i := 0
	for i < len(blocks) && strings.HasPrefix(blocks[i], "#") {
		i++
	}
	if i == len(blocks) {
		return strings.TrimSpace(md), ""
	}
	paras := 0
	j := i
	for j < len(blocks) && paras < keepParas {
		if !strings.HasPrefix(blocks[j], "#") {
			paras++
		}
		j++
	}
	return strings.Join(blocks[i:j], "\n\n"), strings.TrimSpace(strings.Join(blocks[:i], "\n\n") + "\n\n" + strings.Join(blocks[j:], "\n\n"))
}
