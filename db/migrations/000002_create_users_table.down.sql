BEGIN;

DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS users;

DROP TRIGGER IF EXISTS set_users_updated_at ON users;
DROP TRIGGER IF EXISTS set_accounts_updated_at ON accounts;

COMMIT;