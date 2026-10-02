-- Daily picks (Game of the Day). Pick rows join game_cards so page views get
-- the full card (embedded as data.GameCards) plus the editorial note.

-- name: GetDailyPick :one
SELECT sqlc.embed(game_cards), p.note_md
FROM game_cards
JOIN daily_picks p ON p.game_id = game_cards.id
WHERE p.pick_date = $1;

-- name: GetLatestPickOnOrBefore :one
SELECT sqlc.embed(game_cards), p.note_md
FROM game_cards
JOIN daily_picks p ON p.game_id = game_cards.id
WHERE p.pick_date <= $1
ORDER BY p.pick_date DESC
LIMIT 1;

-- name: GetPreviousPickDate :one
SELECT MAX(pick_date)::date AS pick_date
FROM daily_picks
WHERE pick_date < $1;

-- name: GetNextPickDate :one
SELECT MIN(pick_date)::date AS pick_date
FROM daily_picks
WHERE pick_date > $1;

-- name: UpsertDailyPick :exec
INSERT INTO daily_picks (pick_date, game_id, note_md)
VALUES ($1, $2, $3)
ON CONFLICT (pick_date) DO UPDATE
SET game_id = EXCLUDED.game_id,
    note_md = EXCLUDED.note_md;

-- name: DeleteDailyPick :exec
DELETE FROM daily_picks
WHERE pick_date = $1;

-- name: ListPicksBetween :many
SELECT p.pick_date, p.note_md, g.slug, g.title
FROM daily_picks p
JOIN games g ON g.id = p.game_id
WHERE p.pick_date BETWEEN $1 AND $2
ORDER BY p.pick_date;

-- name: ListGamesForPickRotation :many
-- Candidate games for gems pick auto, best first.
SELECT g.id, g.slug, g.title, g.gem_score
FROM games g
ORDER BY g.gem_score DESC NULLS LAST, g.slug;
