BEGIN;

-- Phase-2 importer columns. Steam/IGDB syncs stamp the game row so refresh
-- jobs and curators can see staleness.
ALTER TABLE games ADD COLUMN trailer_youtube_id TEXT;
ALTER TABLE games ADD COLUMN last_steam_sync_at TIMESTAMPTZ;
ALTER TABLE games ADD COLUMN last_igdb_sync_at TIMESTAMPTZ;
ALTER TABLE developers ADD COLUMN last_igdb_sync_at TIMESTAMPTZ;

COMMIT;
