package email

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reLink     = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)
	reImage    = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	reEmphasis = regexp.MustCompile(`(\s|^)\*([^*]+)\*`)
)

// Text renders the plain-text alternative from the same Doc. It must be
// readable on its own: no raw HTML, every game reachable by a full URL,
// unsubscribe link spelled out (required for bulk mail).
func Text(doc Doc) string {
	var b strings.Builder
	b.WriteString("◆ INDIE GAME GEMS\n")
	if doc.Subject != "" {
		b.WriteString(doc.Subject + "\n")
	}
	b.WriteString("\n")
	for _, blk := range doc.Blocks {
		switch {
		case blk.ProseText != "":
			b.WriteString(blk.ProseText + "\n\n")
		case blk.Gotd != nil:
			writeCardText(&b, blk.Gotd.Game, blk.Gotd.Heading)
			if blk.Gotd.NoteText != "" {
				b.WriteString(wrapText(blk.Gotd.NoteText) + "\n")
			}
			b.WriteString("\n")
		case blk.Radar != nil:
			writeSectionText(&b, blk.Radar, false)
		case blk.Top != nil:
			writeSectionText(&b, blk.Top, true)
		case blk.Game != nil:
			writeCardText(&b, *blk.Game, "")
		}
	}
	f := doc.Footer
	b.WriteString("──────────\n")
	if f.BrowserURL != "" {
		b.WriteString("Read in the browser: " + f.BrowserURL + "\n")
	}
	if f.Reason != "" {
		b.WriteString(f.Reason + "\n")
	}
	if f.PostalAddress != "" {
		b.WriteString("Indie Game Gems · " + f.PostalAddress + "\n")
	}
	if f.UnsubURL != "" {
		b.WriteString("Unsubscribe: " + f.UnsubURL + "\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func writeCardText(b *strings.Builder, g GameCard, heading string) {
	if heading != "" {
		b.WriteString(heading + "\n")
	}
	if g.Title != "" {
		b.WriteString(g.Title + "\n")
	}
	if meta := metaLine(g); meta != "" {
		b.WriteString(meta + "\n")
	}
	if g.Tagline != "" {
		b.WriteString(wrapText(g.Tagline) + "\n")
	}
	if g.GameURL != "" {
		b.WriteString("Read more: " + g.GameURL + "\n")
	}
	for _, s := range g.Stores {
		b.WriteString(s.Label + ": " + s.URL + "\n")
	}
}

func writeSectionText(b *strings.Builder, s *Section, ranked bool) {
	if s.Heading != "" {
		b.WriteString(s.Heading + "\n")
	}
	for i, g := range s.Items {
		prefix := " * "
		if ranked {
			prefix = fmt.Sprintf("%2d. ", i+1)
		}
		line := prefix + g.Title
		if meta := metaLine(g); meta != "" {
			line += " — " + meta
		}
		b.WriteString(line + "\n")
		if g.GameURL != "" {
			b.WriteString("   " + g.GameURL + "\n")
		}
	}
	b.WriteString("\n")
}

// metaLine is the shared "date · developer · score" descriptor.
func metaLine(g GameCard) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{g.ReleaseLine, g.Developer, g.ScoreLine} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

// ConfirmText is the plain-text confirmation mail.
func ConfirmText(doc ConfirmDoc) string {
	var b strings.Builder
	b.WriteString("◆ INDIE GAME GEMS\n\n")
	b.WriteString("One click and you're in\n\n")
	b.WriteString("Confirm your email address to start getting Indie Game Gems —\none great indie game a day, plus the weekly release radar.\n\n")
	b.WriteString("Confirm: " + doc.ConfirmURL + "\n\n")
	if doc.Reason != "" {
		b.WriteString(doc.Reason + "\n")
	}
	if doc.PostalAddress != "" {
		b.WriteString("Indie Game Gems · " + doc.PostalAddress + "\n")
	}
	return b.String()
}

// wrapText hard-wraps at ~72 columns on word boundaries so the text part
// survives clients that don't reflow.
func wrapText(s string, width ...int) string {
	const def = 72
	w := def
	if len(width) > 0 && width[0] > 10 {
		w = width[0]
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case len(line)+1+len(word) <= w:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
