package steam

import (
	"testing"
	"time"
)

func TestParseReleaseDate(t *testing.T) {
	tests := []struct {
		in         string
		comingSoon bool
		want       time.Time
		wantOK     bool
	}{
		{"Sep 17, 2020", false, time.Date(2020, 9, 17, 0, 0, 0, 0, time.UTC), true},
		{"17 Sep, 2020", false, time.Date(2020, 9, 17, 0, 0, 0, 0, time.UTC), true},
		{"Feb 24, 2017", false, time.Date(2017, 2, 24, 0, 0, 0, 0, time.UTC), true},
		{"Sep 2020", false, time.Date(2020, 9, 1, 0, 0, 0, 0, time.UTC), true},
		{"2020", false, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"Dec 31, 9998", true, time.Time{}, false},  // sentinel
		{"Dec 31, 9998", false, time.Time{}, false}, // sentinel even without flag
		{"Coming soon", true, time.Time{}, false},
		{"TBA", true, time.Time{}, false},
		{"Q1 2026", false, time.Time{}, false},
		{"", false, time.Time{}, false},
		{"   ", false, time.Time{}, false},
		{"garbage date!", false, time.Time{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := ParseReleaseDate(tt.in, tt.comingSoon)
			if ok != tt.wantOK || !got.Equal(tt.want) {
				t.Errorf("ParseReleaseDate(%q, %v) = %v, %v; want %v, %v",
					tt.in, tt.comingSoon, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
