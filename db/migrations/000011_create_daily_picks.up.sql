BEGIN;

-- Game of the Day, one row per date (primary key = the date it ran).
CREATE TABLE daily_picks (
    pick_date DATE PRIMARY KEY,
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    -- Editorial note shown with the pick (markdown).
    note_md TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER set_daily_picks_updated_at
BEFORE UPDATE ON daily_picks
FOR EACH ROW EXECUTE PROCEDURE set_current_timestamp_updated_at();

-- Adjacent-pick lookups (previous/next navigation).
CREATE INDEX idx_daily_picks_date ON daily_picks (pick_date);

COMMIT;
