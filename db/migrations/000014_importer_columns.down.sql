BEGIN;

ALTER TABLE games DROP COLUMN IF EXISTS trailer_youtube_id;
ALTER TABLE games DROP COLUMN IF EXISTS last_steam_sync_at;
ALTER TABLE games DROP COLUMN IF EXISTS last_igdb_sync_at;
ALTER TABLE developers DROP COLUMN IF EXISTS last_igdb_sync_at;

COMMIT;
