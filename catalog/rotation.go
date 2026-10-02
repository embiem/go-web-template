package catalog

import "strings"

// PickRotation chooses the Game of the Day for `days` consecutive days from
// candidates (best first, e.g. ordered by gem_score). Games in `recent`
// (recently picked) are avoided; a game is never picked twice in one run.
// If every non-recent candidate is exhausted, the rotation wraps around,
// again avoiding `recent` where possible.
func PickRotation(candidates []string, recent map[string]bool, days int) []string {
	picked := make([]string, 0, days)
	run := map[string]bool{}
	for range days {
		choice := ""
		// Pass 1: skip recently picked and already picked in this run.
		for _, c := range candidates {
			if !recent[c] && !run[c] {
				choice = c
				break
			}
		}
		// Pass 2: everything is used up — wrap around in candidate order
		// starting where the run left off, still avoiding recently picked
		// games where possible.
		if choice == "" && len(candidates) > 0 {
			// Prefer the least-recently-used non-recent candidate.
			start := len(picked) % len(candidates)
			for _, c := range append(candidates[start:], candidates[:start]...) {
				if !recent[c] {
					choice = c
					break
				}
			}
			if choice == "" {
				choice = candidates[start]
			}
		}
		// Pass 3: nothing left at all — reuse the best candidate.
		if choice == "" && len(candidates) > 0 {
			choice = candidates[0]
		}
		if choice == "" {
			break // no candidates at all
		}
		picked = append(picked, choice)
		run[choice] = true
	}
	return picked
}

// ParseDate validates a strict YYYY-MM-DD date and returns its parts, for
// CLI input validation without importing a date parser twice.
func ParseDate(s string) (y, m, d int, err error) {
	if err = validDate(s); err != nil {
		return 0, 0, 0, err
	}
	y, _ = atoi(s[:4])
	m, _ = atoi(s[5:7])
	d, _ = atoi(s[8:10])
	return y, m, d, nil
}

func atoi(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, &numError{s}
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

type numError struct{ s string }

func (e *numError) Error() string { return "not a number: " + e.s }

// NormalizeTagSlug turns a curator tag like "Roguelike" or "twin-stick shooter"
// into a canonical slug.
func NormalizeTagSlug(tag string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), " ", "-"))
}
