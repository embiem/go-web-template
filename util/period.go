package util

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Period helpers for the public page URLs (Game of the Day dates, release
// radar ISO weeks, Top of the Month months). Everything works in UTC.

// ParseDate validates a strict YYYY-MM-DD date and returns it as UTC midnight.
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	return t.UTC(), nil
}

// isoWeeksInYear returns the number of ISO weeks (52 or 53) in a year.
// A year has 53 weeks when it starts on a Thursday, or is a leap year
// starting on a Wednesday.
func isoWeeksInYear(year int) int {
	p := func(y int) int { return (y + y/4 - y/100 + y/400) % 7 }
	if p(year) == 4 || p(year-1) == 3 {
		return 53
	}
	return 52
}

// isoWeekMonday returns the Monday 00:00 UTC that starts ISO week `week` of
// `year`. Jan 4 always falls in week 1 (ISO 8601), so the week-1 Monday is
// found from it.
func isoWeekMonday(year, week int) time.Time {
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	week1Monday := jan4.AddDate(0, 0, -((int(jan4.Weekday()) + 6) % 7))
	return week1Monday.AddDate(0, 0, (week-1)*7)
}

// ParseISOWeek validates a strict YYYY-Www string ("2026-W40") and returns the
// Monday 00:00 UTC that starts the week. Week 53 is only valid in years that
// actually have one, so "2025-W53" is rejected.
func ParseISOWeek(s string) (time.Time, error) {
	yearS, weekS, ok := strings.Cut(s, "-W")
	if !ok || len(yearS) != 4 || len(weekS) != 2 {
		return time.Time{}, fmt.Errorf("invalid ISO week %q: want YYYY-Www", s)
	}
	year, err1 := strconv.Atoi(yearS)
	week, err2 := strconv.Atoi(weekS)
	if err1 != nil || err2 != nil || year < 1 {
		return time.Time{}, fmt.Errorf("invalid ISO week %q: want YYYY-Www", s)
	}
	if week < 1 || week > isoWeeksInYear(year) {
		return time.Time{}, fmt.Errorf("invalid ISO week %q", s)
	}
	return isoWeekMonday(year, week), nil
}

// FormatISOWeekLabel renders a Monday as its ISO week label "2026-W40".
func FormatISOWeekLabel(monday time.Time) string {
	year, week := monday.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// ShiftISOWeek returns the Monday ± n weeks (handles year boundaries).
func ShiftISOWeek(monday time.Time, weeks int) time.Time {
	return monday.AddDate(0, 0, 7*weeks)
}

// MonthRange returns [first DAY, first of NEXT month) for 1..12.
func MonthRange(year, month int) (time.Time, time.Time) {
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	return first, first.AddDate(0, 1, 0)
}

// ShiftMonth returns (year, month) ± n months. Uses time.Date arithmetic so
// any n (including |n| ≥ 12) wraps correctly.
func ShiftMonth(year, month, n int) (int, int) {
	t := time.Date(year, time.Month(month)+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	return t.Year(), int(t.Month())
}

// ParseYearMonth validates year (4 digits) and month (1-12).
func ParseYearMonth(yearS, monthS string) (int, int, error) {
	year, err1 := strconv.Atoi(yearS)
	month, err2 := strconv.Atoi(monthS)
	if err1 != nil || err2 != nil || len(yearS) != 4 || month < 1 || month > 12 {
		return 0, 0, fmt.Errorf("invalid month %q/%q: want /top/YYYY/MM", yearS, monthS)
	}
	return year, month, nil
}

// FormatMonthLabel renders "2026", "9" as "September 2026".
func FormatMonthLabel(year, month int) string {
	return time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).Format("January 2006")
}

// FormatDateLabel renders a UTC date as "Sep 29, 2026".
func FormatDateLabel(t time.Time) string {
	return t.Format("Jan 2, 2006")
}

// TodayUTC returns the day containing now, truncated to midnight UTC — the
// site-wide convention for "today" (picks, radar weeks, GOTD default).
func TodayUTC(now time.Time) time.Time {
	return now.UTC().Truncate(24 * time.Hour)
}
