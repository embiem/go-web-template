package view

import (
	"fmt"
	"hash/fnv"
)

// TileGradient returns a deterministic, deep two-tone gradient for games
// without downloaded art yet. It keys off the slug so a game always gets the
// same tile, and pairs a muted dark hue with a brighter sibling so cards
// still look intentional next to real cover art.
func TileGradient(key string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	hue := int(h.Sum32()) % 360
	hue2 := (hue + 42) % 360
	return fmt.Sprintf(
		"background-image:linear-gradient(135deg, hsl(%d 45%% 24%%), hsl(%d 60%% 34%%))",
		hue, hue2,
	)
}
