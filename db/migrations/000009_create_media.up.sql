BEGIN;

-- Locally cached media for games (screenshots, trailers, art). `key` is a
-- path relative to the media cache root, e.g. "games/hades/header.jpg".
-- `source_url` keeps the remote original for refreshes; display always goes
-- through view.MediaURL(key).
CREATE TABLE media (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    -- header | capsule | hero | cover | screenshot | trailer
    kind TEXT NOT NULL CHECK (kind IN ('header', 'capsule', 'hero', 'cover', 'screenshot', 'trailer')),
    key TEXT NOT NULL UNIQUE,
    source_url TEXT NOT NULL DEFAULT '',
    position SMALLINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER set_media_updated_at
BEFORE UPDATE ON media
FOR EACH ROW EXECUTE PROCEDURE set_current_timestamp_updated_at();

-- Ordered listing per game and kind (game cards take the first capsule/header).
CREATE INDEX idx_media_game_kind ON media (game_id, kind, position);

COMMIT;
