package email

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

// RenderHTML renders Doc into email-safe HTML (validated).
func RenderHTML(doc Doc) (string, error) {
	if err := doc.Validate(); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := Page(doc).Render(context.Background(), &buf); err != nil {
		return "", fmt.Errorf("render email html: %w", err)
	}
	return buf.String(), nil
}

// RenderConfirmHTML renders the confirmation mail.
func RenderConfirmHTML(doc ConfirmDoc) (string, error) {
	if doc.ConfirmURL == "" {
		return "", fmt.Errorf("confirm email: ConfirmURL is required")
	}
	var buf bytes.Buffer
	if err := ConfirmPage(doc).Render(context.Background(), &buf); err != nil {
		return "", fmt.Errorf("render confirm html: %w", err)
	}
	return buf.String(), nil
}

// fmtInt is a tiny helper for templ expressions.
func fmtInt(n int) string { return strconv.Itoa(n) }

// Prose converts issue markdown into email-safe HTML: gomarkdown output with
// the styles email clients need inlined onto the generated elements. (No
// external CSS: Gmail and Outlook ignore or strip <style> rules variably.)
//
// Unlike util.MdToHTML this keeps the renderer's newlines: stripping them
// would glue soft-wrapped words together ("a\ntwo-person" → "atwo-person").
func Prose(md string) string {
	if strings.TrimSpace(md) == "" {
		return ""
	}
	ext := parser.CommonExtensions | parser.AutoHeadingIDs
	doc := parser.NewWithExtensions(ext).Parse([]byte(md))
	renderer := html.NewRenderer(html.RendererOptions{Flags: html.CommonFlags | html.HrefTargetBlank})
	rendered := string(markdown.Render(doc, renderer))
	r := strings.NewReplacer(
		"<p>", `<p style="margin:0 0 14px 0;color:#2b2622;font-size:15px;">`,
		"<a href=", `<a style="color:#8b3fe6;text-decoration:underline;" href=`,
		"<strong>", `<strong style="color:#1f1b16;">`,
		"<h1>", `<h1 style="margin:18px 0 8px 0;font-size:20px;color:#2b2622;">`,
		"<h2>", `<h2 style="margin:18px 0 8px 0;font-size:18px;color:#2b2622;">`,
		"<h3>", `<h3 style="margin:16px 0 6px 0;font-size:16px;color:#2b2622;">`,
		"<ul>", `<ul style="margin:0 0 14px 0;padding-left:22px;">`,
		"<ol>", `<ol style="margin:0 0 14px 0;padding-left:22px;">`,
		"<li>", `<li style="margin:0 0 6px 0;">`,
		"<blockquote>", `<blockquote style="margin:0 0 14px 0;padding:2px 0 2px 14px;border-left:3px solid #d8ccb2;color:#6f675c;">`,
		"<hr>", `<hr style="border:0;border-top:1px solid #e3d9c6;margin:20px 0;"/>`,
		"<hr />", `<hr style="border:0;border-top:1px solid #e3d9c6;margin:20px 0;"/>`,
	)
	return r.Replace(rendered)
}

// ProseText converts issue markdown to the plain-text alternative. Links are
// kept readable as "label (url)", emphasis markers dropped.
func ProseText(md string) string {
	lines := strings.Split(strings.TrimSpace(md), "\n")
	var out []string
	for _, line := range lines {
		l := strings.TrimRight(line, " \t")
		trimmed := strings.TrimSpace(l)
		switch {
		case trimmed == "":
			out = append(out, "")
		case strings.HasPrefix(trimmed, "######"), strings.HasPrefix(trimmed, "#####"),
			strings.HasPrefix(trimmed, "####"), strings.HasPrefix(trimmed, "###"),
			strings.HasPrefix(trimmed, "##"), strings.HasPrefix(trimmed, "#"):
			out = append(out, mdTextInline(strings.TrimLeft(trimmed, "# ")))
		case trimmed == "---" || trimmed == "***" || trimmed == "___":
			out = append(out, "──────────")
		default:
			out = append(out, mdTextInline(l))
		}
	}
	text := strings.Join(out, "\n")
	return strings.TrimSpace(text)
}

// mdTextInline rewrites inline markdown to text.
func mdTextInline(s string) string {
	// images first: ![alt](url) → [image: alt]
	s = reImage.ReplaceAllString(s, "[image: $1]")
	// links: [label](url) → label (url)
	s = reLink.ReplaceAllString(s, "$1 ($2)")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = reEmphasis.ReplaceAllString(s, "$1$2")
	s = strings.ReplaceAll(s, "`", "")
	return s
}
