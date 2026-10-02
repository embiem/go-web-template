BEGIN;

-- Game <-> developer/publisher relation. Companies live in `developers` even
-- when they only act as publishers.
CREATE TABLE game_developers (
    game_id UUID NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    developer_id UUID NOT NULL REFERENCES developers(id) ON DELETE CASCADE,
    -- developer | publisher
    role TEXT NOT NULL CHECK (role IN ('developer', 'publisher')),
    PRIMARY KEY (game_id, developer_id, role)
);

CREATE INDEX idx_game_developers_developer ON game_developers (developer_id, role);

COMMIT;
