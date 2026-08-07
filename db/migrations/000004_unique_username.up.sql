BEGIN;

-- Usernames are unique case-insensitively: without this, two concurrent
-- signups both pass an application-level check and GetUserByUsername (:one)
-- fails forever afterwards, and "Admin" could impersonate "admin".
CREATE UNIQUE INDEX IF NOT EXISTS users_username_lower_key ON users (LOWER(username));

COMMIT;
