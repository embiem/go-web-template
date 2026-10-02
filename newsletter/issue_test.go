package newsletter

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseIssueOK(t *testing.T) {
	raw := `---
subject: "Weekly gems ✨"
preheader: "Six releases, one gem"
send_at: 2026-10-06T09:00:00+02:00
---

Hello **friends**, check this out.

{{gotd 2026-09-28}}

## Radar

{{radar 2026-W40}}

{{top 2026-09}}

{{game hollow-knight}}
`
	iss, err := ParseIssue("2026-w40", raw)
	if err != nil {
		t.Fatalf("ParseIssue: %v", err)
	}
	if iss.Subject != "Weekly gems ✨" || iss.Preheader != "Six releases, one gem" {
		t.Fatalf("front matter parsed wrong: %+v", iss)
	}
	if iss.SendAt == nil || iss.SendAt.Month() != time.October || iss.SendAt.Day() != 6 {
		t.Fatalf("send_at parsed wrong: %v", iss.SendAt)
	}
	var kinds []DirectiveKind
	for _, s := range iss.Segments {
		if s.Prose != "" {
			kinds = append(kinds, "")
			continue
		}
		kinds = append(kinds, s.Directive.Kind)
	}
	want := []DirectiveKind{"", KindGotd, "", KindRadar, KindTop, KindGame}
	if len(kinds) != len(want) {
		t.Fatalf("segments = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("segment %d = %q, want %q (all: %v)", i, kinds[i], want[i], kinds)
		}
	}
	if iss.Raw != raw {
		t.Fatalf("Raw not preserved")
	}
}

func TestParseIssueErrors(t *testing.T) {
	cases := []struct {
		name, raw, wantErr string
	}{
		{
			name:    "unknown directive",
			raw:     "---\nsubject: s\n---\n\n{{blast 2026}}\n",
			wantErr: "unknown directive",
		},
		{
			name:    "radar without arg",
			raw:     "---\nsubject: s\n---\n\n{{radar}}\n",
			wantErr: "radar needs an ISO week",
		},
		{
			name:    "radar with bad week",
			raw:     "---\nsubject: s\n---\n\n{{radar 2029-W53}}\n", // 2029 has 52 ISO weeks
			wantErr: "no ISO week 53",
		},
		{
			name:    "top with bad month",
			raw:     "---\nsubject: s\n---\n\n{{top 2026-13}}\n",
			wantErr: "invalid month",
		},
		{
			name:    "game with bad slug",
			raw:     "---\nsubject: s\n---\n\n{{game Hollow Knight}}\n",
			wantErr: "not a valid slug",
		},
		{
			name:    "gotd with bad date",
			raw:     "---\nsubject: s\n---\n\n{{gotd sept 28}}\n",
			wantErr: "YYYY-MM-DD",
		},
		{
			name:    "inline directive",
			raw:     "---\nsubject: s\n---\n\nPlay this now: {{game hades}}!\n",
			wantErr: "on its own line",
		},
		{
			name:    "missing subject",
			raw:     "---\npreheader: x\n---\n\nhi\n",
			wantErr: "subject is required",
		},
		{
			name:    "missing front matter",
			raw:     "just some prose",
			wantErr: "missing YAML front matter",
		},
		{
			name:    "bad send_at",
			raw:     "---\nsubject: s\nsend_at: tomorrow\n---\n",
			wantErr: "RFC 3339",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseIssue("test-slug", tc.raw)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// Multiple problems are reported together, not one per attempt.
func TestParseIssueCollectsErrors(t *testing.T) {
	raw := "---\nsubject: s\n---\n\n{{blast}}\n{{radar}}\nPlay now: {{game hades}}\n"
	_, err := ParseIssue("x", raw)
	if err == nil {
		t.Fatal("want errors")
	}
	for _, want := range []string{"unknown directive", "radar needs", "on its own line"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestParseIssueSlug(t *testing.T) {
	for _, slug := range []string{"2026-W40", "-leading", "under_score", ""} {
		if _, err := ParseIssue(slug, "---\nsubject: s\n---\n"); err == nil {
			t.Fatalf("slug %q accepted", slug)
		}
	}
}

func TestParseISOWeek(t *testing.T) {
	// 2026-W40: Monday Sep 28; end is the following Monday (exclusive),
	// so the week covers Sep 28 … Oct 4 (checked against date -d).
	start, end, err := ParseISOWeek("2026-W40")
	if err != nil {
		t.Fatalf("ParseISOWeek: %v", err)
	}
	if start.Format("2006-01-02") != "2026-09-28" || end.Format("2006-01-02") != "2026-10-05" {
		t.Fatalf("W40 = %s..%s, want 2026-09-28..2026-10-05 (exclusive)", start, end)
	}
	// ISO week 1 of 2026 starts Mon Dec 29, 2025.
	start, end, err = ParseISOWeek("2026-W01")
	if err != nil {
		t.Fatalf("ParseISOWeek W01: %v", err)
	}
	if start.Format("2006-01-02") != "2025-12-29" {
		t.Fatalf("2026-W01 starts %s, want 2025-12-29", start)
	}
	if _, _, err := ParseISOWeek("2026-W40x"); err == nil {
		t.Fatal("trailing junk accepted")
	}
	if _, _, err := ParseISOWeek("40-W2026"); err == nil {
		t.Fatal("nonsense accepted")
	}
}

func TestParseMonth(t *testing.T) {
	start, end, err := ParseMonth("2026-09")
	if err != nil {
		t.Fatalf("ParseMonth: %v", err)
	}
	if start.Format("2006-01-02") != "2026-09-01" || end.Format("2006-01-02") != "2026-10-01" {
		t.Fatalf("2026-09 = %s..%s", start, end)
	}
	if _, _, err := ParseMonth("2026-9"); err == nil {
		t.Fatal("single-digit month accepted")
	}
}

func TestNormalizeAndValidateEmail(t *testing.T) {
	ok := map[string]string{
		"  Reader@Example.COM ":   "reader@example.com",
		"a.b+c@sub.example.co.uk": "a.b+c@sub.example.co.uk",
	}
	for in, want := range ok {
		got, err := NormalizeAndValidate(in)
		if err != nil {
			t.Fatalf("NormalizeAndValidate(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
	bad := []string{
		"", "nope", "a@b", "display name <a@b.com>", "a(b)@c.com",
		"two@@at.com", "a@b..com", "a@.com", "a@com.", "a b@c.com",
		"'" + strings.Repeat("x", 250) + "'@example.com",
	}
	for _, in := range bad {
		if _, err := NormalizeAndValidate(in); err == nil {
			t.Fatalf("accepted %q", in)
		}
	}
}

func TestStateTransitions(t *testing.T) {
	st, err := Confirm(StatusPending)
	if err != nil || st != StatusConfirmed {
		t.Fatalf("confirm pending: %v %v", st, err)
	}
	if st, _ := Confirm(StatusConfirmed); st != StatusConfirmed {
		t.Fatal("confirm must be idempotent")
	}
	if _, err := Confirm(StatusUnsubscribed); !errors.Is(err, ErrAlreadyUnsubscribed) {
		t.Fatalf("confirm after unsub = %v, want ErrAlreadyUnsubscribed", err)
	}
	for _, cur := range []Status{StatusPending, StatusConfirmed, StatusUnsubscribed} {
		st, err := Unsubscribe(cur)
		if err != nil || st != StatusUnsubscribed {
			t.Fatalf("unsubscribe %v: %v %v", cur, st, err)
		}
	}
	if st, _ := Unsubscribe(StatusUnsubscribed); st != StatusUnsubscribed {
		t.Fatal("unsubscribe must be idempotent")
	}
	if st, _ := Resubscribe(StatusUnsubscribed); st != StatusPending {
		t.Fatal("resubscribe must go to pending (re-consent)")
	}
	if st, _ := Resubscribe(StatusConfirmed); st != StatusConfirmed {
		t.Fatal("resubscribe confirmed must be a no-op")
	}
	if _, err := Resubscribe(Status("bogus")); err == nil {
		t.Fatal("bogus state accepted")
	}
}
