BEGIN;

-- Index the daily_picks.game_id FK: games deletes cascade into this table
-- and would otherwise seq-scan it.
CREATE INDEX idx_daily_picks_game ON daily_picks (game_id);

COMMIT;
