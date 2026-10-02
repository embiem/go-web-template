BEGIN;

CREATE TABLE developers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    bio_md TEXT NOT NULL DEFAULT '',
    website_url TEXT NOT NULL DEFAULT '',
    logo_key TEXT NOT NULL DEFAULT '',
    -- Known social profiles, e.g. {"x": "...", "bluesky": "..."}.
    -- Allowed keys: x, bluesky, mastodon, youtube, discord, twitch, instagram.
    socials JSONB NOT NULL DEFAULT '{}'::jsonb,
    igdb_id BIGINT UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER set_developers_updated_at
BEFORE UPDATE ON developers
FOR EACH ROW EXECUTE PROCEDURE set_current_timestamp_updated_at();

COMMIT;
