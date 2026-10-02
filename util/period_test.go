package util

import (
	"testing"
	"time"
)

func mustUTC(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestParseDate(t *testing.T) {
	if d, err := ParseDate("2026-10-01"); err != nil || d != mustUTC(2026, 10, 1) {
		t.Errorf("ParseDate(2026-10-01) = %v, %v", d, err)
	}
	for _, bad := range []string{"", "2026-10-1", "26-10-01", "2026-13-01", "2026-00-10", "2026-10-32", "2026/10/01", "2026-10-01x", "x026-10-01"} {
		if _, err := ParseDate(bad); err == nil {
			t.Errorf("ParseDate(%q) accepted, want error", bad)
		}
	}
}

func TestParseISOWeek(t *testing.T) {
	tests := []struct {
		in      string
		monday  string
		wantErr bool
	}{
		{"2026-W40", "2026-09-28", false},
		// 2026 has 53 ISO weeks; its W53 spans Dec 28 - Jan 3 2027.
		{"2026-W53", "2026-12-28", false},
		{"2027-W01", "2027-01-04", false},
		{"2025-W01", "2024-12-30", false},
		{"2015-W01", "2014-12-29", false},
		// 2025 starts on a Wednesday and is not a leap year: only 52 weeks.
		{"2025-W53", "", true},
		{"2026-W54", "", true},
		{"2026-W00", "", true},
		{"2026-W40 ", "", true},
		{"2026W40", "", true},
		{"2026-w40", "", true},
		{"26-W40", "", true},
		{"2026-W4", "", true},
		{"abcd-W40", "", true},
	}
	for _, tt := range tests {
		got, err := ParseISOWeek(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseISOWeek(%q) accepted, want error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseISOWeek(%q): %v", tt.in, err)
			continue
		}
		if want := mustUTCDate(tt.monday); !got.Equal(want) {
			t.Errorf("ParseISOWeek(%q) monday = %v, want %v", tt.in, got, want)
		}
	}
}

func mustUTCDate(s string) time.Time {
	d, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestISOWeekNeighbours(t *testing.T) {
	// 2026-W53 -> next 2027-W01 (year boundary), prev 2026-W52.
	mon53, err := ParseISOWeek("2026-W53")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := FormatISOWeekLabel(ShiftISOWeek(mon53, 1)), "2027-W01"; got != want {
		t.Errorf("next after 2026-W53 = %q, want %q", got, want)
	}
	if got, want := FormatISOWeekLabel(ShiftISOWeek(mon53, -1)), "2026-W52"; got != want {
		t.Errorf("prev of 2026-W53 = %q, want %q", got, want)
	}
	// 2027-W01 -> prev 2026-W53.
	mon1, _ := ParseISOWeek("2027-W01")
	if got, want := FormatISOWeekLabel(ShiftISOWeek(mon1, -1)), "2026-W53"; got != want {
		t.Errorf("prev of 2027-W01 = %q, want %q", got, want)
	}
	// Week 1 starts Jan 4 2027 and ends Jan 10.
	end := ShiftISOWeek(mon1, 1)
	if end != mustUTC(2027, time.January, 11) {
		t.Errorf("2027-W01 ends %v, want Jan 11", end)
	}
}

func TestWeekMondayConsistency(t *testing.T) {
	// Every parsed week must start on a Monday and its ISOWeek must round-trip.
	for _, s := range []string{"2020-W01", "2024-W52", "2026-W53", "2030-W26"} {
		mon, err := ParseISOWeek(s)
		if err != nil {
			t.Fatal(err)
		}
		if mon.Weekday() != time.Monday {
			t.Errorf("%s starts on %v, want Monday", s, mon.Weekday())
		}
		if got := FormatISOWeekLabel(mon); got != s {
			t.Errorf("label(%v) = %q, want %q", mon, got, s)
		}
	}
}

func TestMonthRangeAndShift(t *testing.T) {
	first, next := MonthRange(2026, 9)
	if !first.Equal(mustUTC(2026, time.September, 1)) || !next.Equal(mustUTC(2026, time.October, 1)) {
		t.Errorf("MonthRange(2026,9) = %v..%v", first, next)
	}
	// December wraps into next year.
	first, next = MonthRange(2026, 12)
	if !first.Equal(mustUTC(2026, time.December, 1)) || !next.Equal(mustUTC(2027, time.January, 1)) {
		t.Errorf("MonthRange(2026,12) = %v..%v", first, next)
	}
	tests := []struct {
		y, m, n, wy, wm int
	}{
		{2026, 10, -1, 2026, 9},
		{2026, 1, -1, 2025, 12},
		{2026, 12, 1, 2027, 1},
		{2026, 11, 2, 2027, 1},
		{2026, 2, -3, 2025, 11},
	}
	for _, tt := range tests {
		if y, m := ShiftMonth(tt.y, tt.m, tt.n); y != tt.wy || m != tt.wm {
			t.Errorf("ShiftMonth(%d,%d,%d) = %d,%d, want %d,%d", tt.y, tt.m, tt.n, y, m, tt.wy, tt.wm)
		}
	}
}

func TestParseYearMonth(t *testing.T) {
	if y, m, err := ParseYearMonth("2026", "9"); err != nil || y != 2026 || m != 9 {
		t.Errorf("ParseYearMonth(2026,9) = %d,%d,%v", y, m, err)
	}
	if y, m, err := ParseYearMonth("2026", "09"); err != nil || y != 2026 || m != 9 {
		t.Errorf("ParseYearMonth(2026,09) = %d,%d,%v", y, m, err)
	}
	for _, bad := range [][2]string{{"2026", "0"}, {"2026", "13"}, {"26", "1"}, {"202", "1"}, {"abcd", "1"}, {"2026", "1x"}} {
		if _, _, err := ParseYearMonth(bad[0], bad[1]); err == nil {
			t.Errorf("ParseYearMonth(%q,%q) accepted, want error", bad[0], bad[1])
		}
	}
}

// "Today" is always UTC, regardless of the local zone (a local-tz default
// made the GOTD date a day early in negative-UTC-offset zones).
func TestTodayUTC(t *testing.T) {
	loc := time.FixedZone("west", -7*3600) // UTC-7: local date is the previous day
	now := time.Date(2026, 10, 1, 23, 30, 0, 0, loc)
	got := TodayUTC(now)
	want := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("TodayUTC = %v, want %v", got, want)
	}
}
