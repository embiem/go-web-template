-- name: CreateUser :one
INSERT INTO users (username, primary_email) 
    VALUES ($1, $2)
RETURNING *;

-- name: CreateAccount :one
INSERT INTO accounts (user_id, provider, access_token, password_hash) 
VALUES (
    $1, $2, $3, $4
) RETURNING *;

-- name: GetUserById :one
SELECT * FROM users
WHERE id = $1;

-- name: GetUserByUsername :one
SELECT * FROM users
WHERE username = $1;

-- name: GetUserAccounts :many
SELECT * FROM accounts
WHERE user_id = $1;
