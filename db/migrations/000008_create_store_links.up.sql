BEGIN;

-- Store links per game. The DB only stores keys relative to the media cache
-- root for display; source_url keeps the remote URL so refreshes can detect
-- changes.
CREATE TABLE store_links (
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    -- steam | gog | epic | itch | humble | direct
    store TEXT NOT NULL CHECK (store IN ('steam', 'gog', 'epic', 'itch', 'humble', 'direct')),
    url TEXT NOT NULL,
    source_url TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (game_id, store)
);

CREATE TRIGGER set_store_links_updated_at
BEFORE UPDATE ON store_links
FOR EACH ROW EXECUTE PROCEDURE set_current_timestamp_updated_at();

COMMIT;
