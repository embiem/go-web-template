-- Page-facing read queries (SELECT only) for the public website handlers.
-- Card-returning queries use the game_cards view so every listing shares
-- data.GameCard. Index notes are per query; the catalog is small, so sorts
-- over a few filtered rows are fine.

-- name: ListCuratedGems :many
-- Home "curated gems": highest-scoring released games. Seq scan + top-N sort
-- is correct here; catalog size makes an index (gem_score) not worth its
-- write amplification (revisit at ~10k games).
SELECT gc.*
FROM game_cards gc
WHERE gc.release_status = 'released'
  AND gc.gem_score IS NOT NULL
ORDER BY gc.gem_score DESC
LIMIT $1;

-- name: ListTopGames :many
-- Top of the Month (page + home section): best games by gem_score released
-- in [from, to). release_date range is served by idx_games_release_date.
SELECT gc.*
FROM game_cards gc
WHERE gc.release_date >= $1 AND gc.release_date < $2
ORDER BY gc.gem_score DESC NULLS LAST
LIMIT $3;

-- name: ListDeveloperGames :many
-- Every game a developer is attached to, in any role. developer_id is served
-- by idx_game_developers_developer; DISTINCT ON dedupes the company being
-- both developer and publisher. Ordered by id for DISTINCT ON; the handler
-- re-sorts newest-first for display.
SELECT DISTINCT ON (gc.id) gc.*
FROM game_cards gc
JOIN game_developers gd
  ON gd.game_id = gc.id
 AND gd.developer_id = $1
ORDER BY gc.id, gd.role;

-- name: ListRecentPicks :many
-- Game of the Day archive + RSS feed: latest picks at or before `before`.
-- pick_date filter/order is served by daily_picks_pkey.
SELECT sqlc.embed(game_cards), p.pick_date, p.note_md
FROM game_cards
JOIN daily_picks p ON p.game_id = game_cards.id
WHERE p.pick_date <= $1
ORDER BY p.pick_date DESC
LIMIT $2;

-- name: GetLatestPickDateOnOrBefore :one
-- The date the "latest pick so far" fallback actually landed on (needed for
-- the canonical URL and date label when today has no pick).
SELECT MAX(pick_date)::date AS pick_date
FROM daily_picks
WHERE pick_date <= $1;

-- name: ListAllDeveloperSlugs :many
-- Sitemap.
SELECT slug FROM developers ORDER BY slug;

-- name: CountGamesReleasedBetween :one
-- Home "Top of the Month" checks whether the current month is thin enough to
-- fall back to the previous one. release_date range: idx_games_release_date.
SELECT COUNT(*)::int AS count
FROM game_cards
WHERE release_date >= $1 AND release_date < $2;

-- name: ListUpcomingGames :many
-- Home radar row filler: next dated releases after `from` (the end of the
-- current week), soonest first. release_date range: idx_games_release_date.
SELECT *
FROM game_cards
WHERE release_date >= $1
ORDER BY release_date ASC
LIMIT $2;

-- name: ListRecentlyReleased :many
-- Home radar row filler, last resort: most recent releases before `before`
-- (the start of the current week). release_date range: idx_games_release_date.
SELECT *
FROM game_cards
WHERE release_date < $1
ORDER BY release_date DESC
LIMIT $2;
