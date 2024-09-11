BEGIN;

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(255) NOT NULL,
    primary_email VARCHAR(255),
    created_at timestamp not null default now(),
    updated_at timestamp not null default now()
);

CREATE TABLE IF NOT EXISTS accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    provider VARCHAR(100) NOT NULL,
    access_token TEXT,
    password_hash TEXT,
    created_at timestamp not null default now(),
    updated_at timestamp not null default now()
);


CREATE TRIGGER set_users_updated_at 
BEFORE UPDATE ON users 
FOR EACH ROW EXECUTE PROCEDURE set_current_timestamp_updated_at();

CREATE TRIGGER set_accounts_updated_at 
BEFORE UPDATE ON accounts 
FOR EACH ROW EXECUTE PROCEDURE set_current_timestamp_updated_at();


COMMIT;