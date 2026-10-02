-- Catalog: games, developers and their relations (page-facing reads).
--
-- Game cards: game_cards is a SQL view (migration 000013) that pre-joins the
-- primary developer (first developer alphabetically) and card media (first
-- header by position, else first capsule — 460x215 headers fit cards best).
-- Card-returning queries select from it so all views share data.GameCards.

-- name: GetGameBySlug :one
SELECT * FROM games
WHERE slug = $1;

-- name: ListGameDevelopers :many
-- All companies attached to a game, with their role (developer/publisher).
SELECT d.*, gd.role
FROM game_developers gd
JOIN developers d ON d.id = gd.developer_id
WHERE gd.game_id = $1
ORDER BY gd.role, d.name;

-- name: ListStoreLinks :many
SELECT * FROM store_links
WHERE game_id = $1
ORDER BY store;

-- name: ListGameMedia :many
SELECT * FROM media
WHERE game_id = $1
ORDER BY kind, position;

-- name: ListGameTags :many
SELECT t.*
FROM game_tags gt
JOIN tags t ON t.id = gt.tag_id
WHERE gt.game_id = $1
ORDER BY t.name;

-- name: GetDeveloperBySlug :one
SELECT * FROM developers
WHERE slug = $1;

-- name: ListDeveloperGameRoles :many
-- Roles a developer has for a given game (0..2 rows).
SELECT gd.role
FROM game_developers gd
WHERE gd.game_id = $1 AND gd.developer_id = $2;

-- name: ListGamesByDeveloper :many
-- "More from this developer" — excludes the game the viewer is on.
SELECT gc.*
FROM game_cards gc
JOIN game_developers gd
  ON gd.game_id = gc.id
 AND gd.developer_id = $1
 AND gd.role = 'developer'
WHERE gc.id != $2
ORDER BY gc.release_date DESC NULLS LAST;

-- name: GetGameCardBySlug :one
SELECT gc.*
FROM game_cards gc
WHERE gc.slug = $1;

-- name: ListGamesReleasedBetween :many
-- Weekly Release Radar: games released in [from, to), ordered by date
-- (exclusive end, so callers pass Monday of the NEXT week).
SELECT gc.*
FROM game_cards gc
WHERE gc.release_date >= $1 AND gc.release_date < $2
ORDER BY gc.release_date;

-- name: ListTopGamesBetween :many
-- Top of the Month: best games by gem_score released in [from, to)
-- (exclusive end, so {{top 2026-09}} never picks up next month's day 1);
-- min_score filters the tail.
SELECT gc.*
FROM game_cards gc
WHERE gc.release_date >= $1 AND gc.release_date < $2
  AND gc.gem_score >= $3
ORDER BY gc.gem_score DESC;

-- name: ListAllGameSlugs :many
-- Sitemap.
SELECT slug FROM games ORDER BY slug;
