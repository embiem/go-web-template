BEGIN;

-- Dropping the tables drops their triggers with them; dropping the triggers
-- afterwards would fail because the tables are already gone.
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS users;

COMMIT;