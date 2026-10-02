package catalog

import "testing"

func f(v float64) *float64 { return &v }

func TestGemScore(t *testing.T) {
	tests := []struct {
		name       string
		editorial  *float64
		steamPct   *float64
		steamCount int64
		want       *float64
	}{
		{"editorial only, below threshold", f(80), f(95), 49, f(80)},
		{"editorial only, zero reviews", f(80), f(95), 0, f(80)},
		{"zero editorial", f(0), f(90), 1000, f(3.4)},
		{"at threshold, zero confidence", f(80), f(100), 50, f(80)},
		{"editorial clamped high", f(120), nil, 0, f(100)},
		{"editorial clamped low", f(-5), nil, 0, f(0)},
		{"steam pct clamped high", f(50), f(120), 1000, f(51.9)},
		{"both perfect", f(100), f(100), 10_000, f(100)},
		{"both worst", f(0), f(0), 10_000, f(0)},
		{"full confidence blend", f(70), f(90), 10_000, f(78)},
		{"ramping confidence at 5025 reviews", f(70), f(90), 5_025, f(74)},
		// NULL editorial (IGDB catalogue imports): Steam-driven or unscored.
		{"no editorial, few reviews stays NULL", nil, f(90), 10, nil},
		{"no editorial, at threshold is steam pct", nil, f(88), 50, f(88)},
		{"no editorial, many reviews is steam pct", nil, f(76.5), 100_000, f(76.5)},
		{"no editorial, pct nil counts as 0", nil, nil, 10_000, f(0)},
		{"no editorial, pct clamped", nil, f(120), 10_000, f(100)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GemScore(tt.editorial, tt.steamPct, tt.steamCount)
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("GemScore(%v, %v, %d) = %v, want nil", tt.editorial, tt.steamPct, tt.steamCount, *got)
			case tt.want != nil && got == nil:
				t.Errorf("GemScore(%v, %v, %d) = nil, want %v", tt.editorial, tt.steamPct, tt.steamCount, *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("GemScore(%v, %v, %d) = %v, want %v", tt.editorial, tt.steamPct, tt.steamCount, *got, *tt.want)
			}
		})
	}
}
