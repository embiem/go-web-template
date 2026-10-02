package main

import (
	"fmt"
	"testing"
	"time"
)

// d builds UTC midnight dates for planPickAuto tests.
func d(day int) time.Time { return time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC) }

// Reproduces the back-to-back-repeat bug: day 0 filled d1..d7 with the top 7
// games; on day 1 the remaining empty day d8 must not get the game already
// scheduled on d7, even though every other candidate is "used up".
func TestPlanPickAutoNoBackToBackRepeats(t *testing.T) {
	order := []string{"a", "b", "c", "d", "e", "f", "g"}
	empty := []time.Time{d(8)}
	recent := map[string]bool{"g": true} // d7's pick (and the rest of the run)

	plan := planPickAuto(empty, order, recent)
	if len(plan) != 1 {
		t.Fatalf("want 1 pick, got %d", len(plan))
	}
	if plan[0].slug == "g" {
		t.Errorf("d8 = %q, repeats d7's pick", plan[0].slug)
	}
	if plan[0].date != d(8) {
		t.Errorf("date = %v, want 2026-10-08", plan[0].date)
	}
}

// Empty days are filled in order, already-filled days are skipped, and the
// plan respects the recent set throughout.
func TestPlanPickAutoSkipsFilledDays(t *testing.T) {
	order := []string{"a", "b", "c"}
	dates := []time.Time{d(2), d(3), d(4), d(5)}
	hasPick := map[string]bool{"2026-10-03": true, "2026-10-04": true}
	recent := map[string]bool{"a": true}

	var empty []time.Time
	for _, dt := range dates {
		if !hasPick[dt.Format("2006-01-02")] {
			empty = append(empty, dt)
		}
	}
	plan := planPickAuto(empty, order, recent)
	got := fmt.Sprint(plan)
	for _, p := range plan {
		if p.slug == "a" {
			t.Errorf("recently picked game a rescheduled: %v", got)
		}
	}
	if len(plan) != 2 || plan[0].date != d(2) || plan[1].date != d(5) {
		t.Errorf("want picks on d2 and d5 only, got %v", got)
	}
}
