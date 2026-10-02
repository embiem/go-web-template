package newsletter

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Issue authoring format
//
// An issue is a markdown file newsletter/issues/<slug>.md:
//
//	---
//	subject: "Indie Game Gems — week 2026-W40"
//	preheader: "Six fresh releases and one gem we love"
//	send_at: 2026-10-06T09:00:00+02:00   # optional
//	---
//
//	Prose markdown… followed by block directives on their own lines:
//
//	{{gotd 2026-09-28}}        Game of the Day (arg: pick date; default today)
//	{{radar 2026-W40}}         releases in the given ISO week
//	{{top 2026-09}}            best games of the given month
//	{{game hollow-knight}}     one game card with store buttons
//
// Unknown directive kinds, malformed arguments and directives used inline
// are parse errors (preview fails fast). Unknown game slugs or periods with
// no data fail at render/preview time, never at send time.

type DirectiveKind string

const (
	KindGotd  DirectiveKind = "gotd"
	KindRadar DirectiveKind = "radar"
	KindTop   DirectiveKind = "top"
	KindGame  DirectiveKind = "game"
)

// Segment is one piece of the issue body: either a prose chunk or a
// directive. Order matters — it is the reading order of the mail.
type Segment struct {
	Prose     string // markdown; empty for directive segments
	Directive Directive
}

type Directive struct {
	Kind DirectiveKind
	Arg  string // empty for gotd
	Line int    // 1-based line in the file
}

// Issue is a parsed issue file.
type Issue struct {
	Slug      string
	Subject   string
	Preheader string
	SendAt    *time.Time
	Raw       string // the full file contents, snapshotted into issues.source_md
	Body      string // markdown after the front matter, directives included
	Segments  []Segment
}

// ParseIssue parses the raw file contents. The slug comes from the file
// name (without .md) and is validated here so URLs stay clean.
func ParseIssue(slug, raw string) (*Issue, error) {
	if !validSlug(slug) {
		return nil, fmt.Errorf("issue slug %q is not a valid slug (want lowercase letters, digits, dashes)", slug)
	}
	fm, body, err := splitFrontMatter(raw)
	if err != nil {
		return nil, err
	}
	iss := &Issue{Slug: slug, Raw: raw, Body: body}
	if fm.Subject == "" {
		return nil, fmt.Errorf("front matter: subject is required")
	}
	iss.Subject = fm.Subject
	iss.Preheader = fm.Preheader
	sendAt, err := parseSendAt(fm.SendAt)
	if err != nil {
		return nil, err
	}
	iss.SendAt = sendAt
	segs, perr := parseSegments(body)
	iss.Segments = segs
	return iss, joinErrors(append(perr, err))
}

type frontMatter struct {
	Subject   string `yaml:"subject"`
	Preheader string `yaml:"preheader"`
	SendAt    string `yaml:"send_at"`
}

func parseSendAt(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("send_at %q must be RFC 3339 or YYYY-MM-DD", s)
}

// splitFrontMatter splits a leading
//
//	---
//	…yaml…
//	---
//
// block off raw. It returns (frontMatter, body, err).
func splitFrontMatter(raw string) (*frontMatter, string, error) {
	r := strings.TrimPrefix(raw, "\ufeff")
	r = strings.TrimLeft(r, "\n")
	if !strings.HasPrefix(r, "---") {
		return nil, "", fmt.Errorf("missing YAML front matter (file must start with ---)")
	}
	rest := strings.TrimPrefix(r[3:], "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, "", fmt.Errorf("unterminated YAML front matter (no closing ---)")
	}
	yamlPart := rest[:end]
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\r")
	body = strings.TrimPrefix(body, "\n")

	fm := &frontMatter{}
	if err := yaml.Unmarshal([]byte(yamlPart), fm); err != nil {
		return nil, "", fmt.Errorf("front matter: %w", err)
	}
	return fm, body, nil
}

var (
	reDirective = regexp.MustCompile(`^\{\{\s*([a-z]+)(?:\s+([^{}]+?))?\s*\}\}$`)
	reSlug      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

// parseSegments scans body lines into prose/directive segments. It collects
// every problem it finds so an editor fixes them in one pass.
func parseSegments(body string) ([]Segment, []error) {
	var (
		segs  []Segment
		errs  []error
		prose strings.Builder
	)
	flush := func() {
		if p := strings.TrimSpace(prose.String()); p != "" {
			segs = append(segs, Segment{Prose: p})
		}
		prose.Reset()
	}
	for i, line := range strings.Split(body, "\n") {
		n := i + 1
		if m := reDirective.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			kind, arg := DirectiveKind(m[1]), strings.TrimSpace(m[2])
			if err := validateDirective(kind, arg); err != nil {
				errs = append(errs, fmt.Errorf("line %d: %w", n, err))
				continue
			}
			flush()
			segs = append(segs, Segment{Directive: Directive{Kind: kind, Arg: arg, Line: n}})
			continue
		}
		// A directive glued into a prose line is almost always a mistake —
		// e.g. "Play this: {{game hades}}".
		if idx := strings.Index(line, "{{"); idx >= 0 && strings.Contains(line[idx:], "}}") {
			errs = append(errs, fmt.Errorf("line %d: directive %q must be on its own line", n, line[strings.Index(line[idx:], "{{"):]))
			continue
		}
		if prose.Len() == 0 {
			prose.WriteString(strings.TrimLeft(line, " "))
		} else {
			prose.WriteString(line)
		}
		prose.WriteString("\n")
	}
	flush()
	return segs, errs
}

func validateDirective(kind DirectiveKind, arg string) error {
	switch kind {
	case KindGotd:
		if arg == "" {
			return nil
		}
		if _, err := ParseDate(arg); err != nil {
			return fmt.Errorf("gotd takes an optional YYYY-MM-DD date, got %q", arg)
		}
		return nil
	case KindRadar:
		if arg == "" {
			return fmt.Errorf("radar needs an ISO week like {{radar 2026-W40}}")
		}
		_, _, err := ParseISOWeek(arg)
		return err
	case KindTop:
		if arg == "" {
			return fmt.Errorf("top needs a month like {{top 2026-09}}")
		}
		_, _, err := ParseMonth(arg)
		return err
	case KindGame:
		if arg == "" {
			return fmt.Errorf("game needs a game slug like {{game hollow-knight}}")
		}
		if !validSlug(arg) {
			return fmt.Errorf("game slug %q is not a valid slug", arg)
		}
		return nil
	default:
		return fmt.Errorf("unknown directive %q (known: gotd, radar, top, game)", kind)
	}
}

func validSlug(s string) bool { return reSlug.MatchString(s) }

// ParseDate parses YYYY-MM-DD.
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	return t, nil
}

// ParseISOWeek parses an ISO week like 2026-W40 into a half-open range
// [Monday, next Monday). Weeks that do not exist in that year (e.g.
// 2027-W53 — 2027 has 52 weeks) are errors.
func ParseISOWeek(s string) (start, end time.Time, err error) {
	yearS, weekS, ok := strings.Cut(s, "-W")
	if !ok {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid week %q: want e.g. 2026-W40", s)
	}
	year, err := strconv.Atoi(yearS)
	if err != nil || year < 1 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid week %q: want e.g. 2026-W40", s)
	}
	week, err := strconv.Atoi(weekS)
	if err != nil || week < 1 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid week %q: want e.g. 2026-W40", s)
	}
	// ISO 8601: week 1 contains the first Thursday; Jan 4 is always in it.
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	mondayWeek1 := jan4.AddDate(0, 0, -isoWeekday(jan4)+1)
	start = mondayWeek1.AddDate(0, 0, (week-1)*7)
	end = start.AddDate(0, 0, 7) // exclusive end: [start, end), same as ParseMonth
	// Round-trip check rejects weeks that don't exist in that year
	// (e.g. 2027-W53 — 2026 does have a week 53).
	if gy, gw := start.ISOWeek(); gy != year || gw != week {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid week %q: year %d has no ISO week %d", s, year, week)
	}
	return start, end, nil
}

// isoWeekday maps time.Weekday to ISO numbering (Monday = 1 … Sunday = 7).
func isoWeekday(t time.Time) int {
	if wd := int(t.Weekday()); wd != 0 {
		return wd
	}
	return 7
}

// ParseMonth parses 2026-09 into the month's first day and the first day of
// the following month (exclusive end).
func ParseMonth(s string) (start, end time.Time, err error) {
	t, e := time.Parse("2006-01", s)
	if e != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid month %q: want e.g. 2026-09", s)
	}
	return t, t.AddDate(0, 1, 0), nil
}

func joinErrors(errs []error) error {
	var clean []error
	for _, e := range errs {
		if e != nil {
			clean = append(clean, e)
		}
	}
	return errors.Join(clean...)
}
