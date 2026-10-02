BEGIN;

CREATE TABLE games (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL,
    tagline TEXT NOT NULL DEFAULT '',
    description_md TEXT NOT NULL DEFAULT '',
    gem_note_md TEXT NOT NULL DEFAULT '',
    -- Exact release date, if known; NULL for unannounced/TBA dates.
    release_date DATE,
    -- announced | upcoming | early_access | released
    release_status TEXT NOT NULL DEFAULT 'announced'
        CHECK (release_status IN ('announced', 'upcoming', 'early_access', 'released')),
    steam_appid BIGINT UNIQUE,
    igdb_id BIGINT UNIQUE,
    website_url TEXT NOT NULL DEFAULT '',
    -- Steam review stats the gem_score derives from.
    steam_review_pct SMALLINT CHECK (steam_review_pct BETWEEN 0 AND 100),
    steam_review_count BIGINT NOT NULL DEFAULT 0,
    -- 0-100 in-house curation score.
    editorial_score SMALLINT CHECK (editorial_score BETWEEN 0 AND 100),
    -- Computed blended score (see catalog.GemScore); recomputed by the CLI
    -- whenever editorial_score / steam stats change. Future: also blended
    -- with user ratings.
    gem_score REAL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER set_games_updated_at
BEFORE UPDATE ON games
FOR EACH ROW EXECUTE PROCEDURE set_current_timestamp_updated_at();

CREATE INDEX idx_games_release_date ON games (release_date);

COMMIT;
