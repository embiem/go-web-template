// Package catalog holds the domain logic for the game catalog: gem_score
// computation, seed-file parsing/validation and daily-pick rotation.
//
// Phase-2 importers (Steam/IGDB) should use the store API in this package
// (backed by db.Queries) so the CLI and future refresh jobs share one code
// path.
package catalog

// GemScore blends our editorial curation with Steam review sentiment into a
// single 0-100 score. It is a pure function so it can be unit-tested and
// recomputed anywhere (CLI rescore, future refresh jobs).
//
// Pass nil for scores the DB doesn't have (editorial_score is NULL for games
// imported from IGDB developer catalogues, steam_review_pct is NULL before
// the first Steam import):
//
//   - no editorial + not enough Steam reviews  -> nil (stay unscored)
//   - no editorial + enough Steam reviews      -> Steam-driven score
//   - editorial + no Steam signal              -> editorial score
//   - both                                     -> weighted blend
//
// Future: this becomes a three-way blend once user ratings exist — add a
// userRating component and re-normalize the weights below.
func GemScore(editorialScore *float64, steamReviewPct *float64, steamReviewCount int64) *float64 {
	steamValid := steamReviewCount >= steamMinReviewCount
	if !steamValid {
		// Not enough Steam reviews to be meaningful: editorial-only fallback.
		return editorialOnly(editorialScore)
	}
	var pct float64
	if steamReviewPct != nil {
		pct = clamp(*steamReviewPct, 0, 100)
	}

	if editorialScore == nil {
		// No curation yet: Steam reviews alone (already >= threshold).
		return ptr(round1(pct))
	}

	// Confidence ramps linearly from 0 at steamMinReviewCount to 1 at
	// steamFullConfidenceCount, so a game with barely-enough reviews doesn't
	// swing the score as hard as one with thousands.
	conf := float64(min(steamReviewCount, int64(steamFullConfidenceCount))-steamMinReviewCount) /
		float64(steamFullConfidenceCount-steamMinReviewCount)

	weighted := (editorialWeight**editorialScore + steamWeight*pct) / (editorialWeight + steamWeight)
	return ptr(round1((1-conf)**editorialScore + conf*weighted))
}

func editorialOnly(editorialScore *float64) *float64 {
	if editorialScore == nil {
		return nil
	}
	return ptr(round1(clamp(*editorialScore, 0, 100)))
}

func ptr(v float64) *float64 { return &v }

// Blend weights and Steam thresholds for GemScore.
const (
	editorialWeight = 0.6
	steamWeight     = 0.4

	// Below this many Steam reviews the percentage is ignored entirely.
	steamMinReviewCount = 50
	// At/above this many reviews the Steam signal is trusted at full weight.
	steamFullConfidenceCount = 10_000
)

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}
