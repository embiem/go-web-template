-- CLI upserts / admin (gems seed, link, rescore, games list).

-- name: UpsertDeveloper :one
-- Seed upsert. Seed owns name/socials; importer-owned igdb_id, bio_md,
-- website_url and logo_key are only filled when the seed provides a
-- non-empty value (or never, for igdb_id).
INSERT INTO developers (slug, name, bio_md, website_url, socials, igdb_id)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (slug) DO UPDATE
SET name = EXCLUDED.name,
    bio_md = COALESCE(NULLIF(EXCLUDED.bio_md, ''), developers.bio_md),
    website_url = COALESCE(NULLIF(EXCLUDED.website_url, ''), developers.website_url),
    socials = EXCLUDED.socials,
    igdb_id = COALESCE(EXCLUDED.igdb_id, developers.igdb_id)
RETURNING *;

-- name: UpsertGame :one
-- Seed upsert. Field ownership: seed owns title, tagline, gem_note_md,
-- release_date/status, steam_appid, editorial_score. Importer-owned fields
-- (description_md, website_url) are only overwritten when the seed provides
-- a non-empty value, so re-seeding never wipes Steam/IGDB data. Review
-- stats, igdb_id, trailer and sync timestamps aren't touched at all.
INSERT INTO games (slug, title, tagline, description_md, gem_note_md,
                   release_date, release_status, steam_appid, igdb_id,
                   website_url, editorial_score)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (slug) DO UPDATE
SET title = EXCLUDED.title,
    tagline = EXCLUDED.tagline,
    description_md = COALESCE(NULLIF(EXCLUDED.description_md, ''), games.description_md),
    gem_note_md = EXCLUDED.gem_note_md,
    release_date = EXCLUDED.release_date,
    release_status = EXCLUDED.release_status,
    steam_appid = EXCLUDED.steam_appid,
    igdb_id = COALESCE(EXCLUDED.igdb_id, games.igdb_id),
    website_url = COALESCE(NULLIF(EXCLUDED.website_url, ''), games.website_url),
    editorial_score = EXCLUDED.editorial_score
RETURNING *;

-- name: UpsertGameDeveloperRole :exec
INSERT INTO game_developers (game_id, developer_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (game_id, developer_id, role) DO NOTHING;

-- name: DeleteGameDeveloperRoles :exec
-- Seed sync: drop roles no longer present in the seed file.
DELETE FROM game_developers
WHERE game_id = $1 AND developer_id = $2 AND role = $3;

-- name: UpsertTag :one
INSERT INTO tags (slug, name)
VALUES ($1, $2)
ON CONFLICT (slug) DO UPDATE
SET name = EXCLUDED.name
RETURNING *;

-- name: UpsertGameTag :exec
INSERT INTO game_tags (game_id, tag_id)
VALUES ($1, $2)
ON CONFLICT (game_id, tag_id) DO NOTHING;

-- name: UpsertStoreLink :exec
INSERT INTO store_links (game_id, store, url, source_url)
VALUES ($1, $2, $3, $4)
ON CONFLICT (game_id, store) DO UPDATE
SET url = EXCLUDED.url,
    source_url = EXCLUDED.source_url;

-- name: DeleteStoreLink :exec
DELETE FROM store_links
WHERE game_id = $1 AND store = $2;

-- name: SetGameScores :exec
-- Recomputed inputs + gem_score; called by gems rescore and after seed.
UPDATE games
SET editorial_score = $2,
    steam_review_pct = $3,
    steam_review_count = $4,
    gem_score = $5
WHERE id = $1;

-- name: GetGameByID :one
SELECT * FROM games
WHERE id = $1;

-- name: ListGames :many
-- Full catalog rows for gems games list / rescore (scan on purpose).
SELECT * FROM games
ORDER BY title;

-- ============================================================================
-- Phase-2 importers (steam/igdb sync, media fetch)
-- ============================================================================

-- name: ImportStoreLink :exec
-- Importer variant: never overwrites a store link that seed/curation set.
INSERT INTO store_links (game_id, store, url, source_url)
VALUES ($1, $2, $3, $4)
ON CONFLICT (game_id, store) DO NOTHING;

-- name: UpsertMedia :one
-- Media cache row by key; keys are content-addressed paths under MEDIA_DIR.
INSERT INTO media (game_id, kind, key, source_url, position)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (key) DO UPDATE
SET game_id = EXCLUDED.game_id,
    kind = EXCLUDED.kind,
    source_url = EXCLUDED.source_url,
    position = EXCLUDED.position
RETURNING *;

-- name: ListGameMediaKeys :many
SELECT key FROM media
WHERE game_id = $1;

-- name: SetGameSteamSync :exec
-- Steam importer merge: curated fields always win. Only description, review
-- stats, empty website, and a missing release date/status are filled.
UPDATE games
SET description_md = $2,
    steam_review_pct = $3,
    steam_review_count = $4,
    website_url = CASE WHEN website_url = '' THEN $5 ELSE website_url END,
    release_date = COALESCE(release_date, $6),
    release_status = CASE
        WHEN release_date IS NULL AND release_status = 'announced' THEN $7
        ELSE release_status END,
    last_steam_sync_at = now()
WHERE id = $1;

-- name: SetGameIGDBSync :exec
-- IGDB importer merge: never clobber an existing igdb_id or trailer.
UPDATE games
SET igdb_id = COALESCE(igdb_id, $2),
    trailer_youtube_id = COALESCE(trailer_youtube_id, $3),
    last_igdb_sync_at = now()
WHERE id = $1;

-- name: SetDeveloperIGDBSync :exec
-- IGDB importer merge for companies: igdb_id never clobbered; description,
-- website and logo only fill gaps.
UPDATE developers
SET igdb_id = COALESCE(igdb_id, $2),
    bio_md = CASE WHEN bio_md = '' THEN $3 ELSE bio_md END,
    website_url = CASE WHEN website_url = '' THEN $4 ELSE website_url END,
    logo_key = CASE WHEN logo_key = '' THEN $5 ELSE logo_key END,
    last_igdb_sync_at = now()
WHERE id = $1;

-- name: ListGamesForSync :many
-- Steam/IGDB sync + media-fetch candidates (admin projection).
SELECT g.id, g.slug, g.title, g.steam_appid, g.igdb_id, g.trailer_youtube_id,
       g.last_steam_sync_at, g.last_igdb_sync_at
FROM games g
ORDER BY g.slug;

-- name: ListGamesMissingMedia :many
-- Games without a header/capsule card image yet (refresh targets).
SELECT g.id, g.slug, g.steam_appid, g.igdb_id, g.trailer_youtube_id
FROM games g
WHERE NOT EXISTS (
    SELECT 1 FROM media m
    WHERE m.game_id = g.id AND m.kind IN ('header', 'capsule', 'cover')
)
ORDER BY g.slug;

-- name: ListDevelopersWithIGDBID :many
SELECT d.id, d.slug, d.name, d.igdb_id
FROM developers d
WHERE d.igdb_id IS NOT NULL
ORDER BY d.slug;

-- name: GetDeveloperByID :one
SELECT * FROM developers
WHERE id = $1;

-- name: ListDevelopers :many
SELECT * FROM developers
ORDER BY slug;

-- name: GetDeveloperByName :one
SELECT * FROM developers
WHERE LOWER(name) = LOWER($1);

-- name: ImportDeveloper :one
-- Importer variant: never touches an existing developer with the same slug.
INSERT INTO developers (slug, name, igdb_id)
VALUES ($1, $2, $3)
ON CONFLICT (slug) DO NOTHING
RETURNING *;

-- name: SetGameWebsiteIfEmpty :exec
UPDATE games
SET website_url = CASE WHEN website_url = '' THEN $2 ELSE website_url END
WHERE id = $1;

-- name: GetGameBySteamAppID :one
SELECT * FROM games
WHERE steam_appid = $1;
