-- name: GetCredential :one
SELECT * FROM account_credentials WHERE account_id = $1;

-- name: PutCredential :exec
INSERT INTO account_credentials (account_id, password_hash, changed_at)
VALUES ($1, $2, $3)
ON CONFLICT (account_id) DO UPDATE
SET password_hash = excluded.password_hash, changed_at = excluded.changed_at;

-- name: GetSessionByToken :one
SELECT * FROM sessions WHERE token_hash = $1;

-- name: CreateSession :exec
INSERT INTO sessions (id, account_id, token_hash, created_at, last_seen_at, expires_at, impersonator_id)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: UpdateSession :execrows
UPDATE sessions
SET account_id = $2, token_hash = $3, created_at = $4, last_seen_at = $5, expires_at = $6,
    impersonator_id = $7
WHERE id = $1;

-- name: DeleteSession :execrows
DELETE FROM sessions WHERE id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= $1;

-- name: GetAccountToken :one
SELECT * FROM account_tokens WHERE token_hash = $1;

-- name: CreateAccountToken :exec
INSERT INTO account_tokens (token_hash, account_id, purpose, email, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: DeleteAccountToken :execrows
DELETE FROM account_tokens WHERE token_hash = $1;

-- name: DeleteAccountTokensByAccount :exec
DELETE FROM account_tokens WHERE account_id = $1 AND purpose = $2;

-- name: DeleteExpiredAccountTokens :execrows
DELETE FROM account_tokens WHERE expires_at <= $1;
