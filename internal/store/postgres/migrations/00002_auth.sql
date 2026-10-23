-- Authentication and mail: confirmed accounts, passwords, sessions,
-- single-use email tokens and the mail outbox.
--
-- Secret tokens (session cookies, email links) are stored as their SHA-256
-- hash only (domain.HashToken), so a leaked database holds no usable token.
-- Conventions as in 00001_core.sql.

-- +goose Up

ALTER TABLE accounts ADD COLUMN confirmed_at timestamptz;

-- Accounts from before registration existed were created by the school (seed
-- or import), so their addresses count as confirmed.
UPDATE accounts SET confirmed_at = now() WHERE confirmed_at IS NULL;

CREATE TABLE account_credentials (
    account_id uuid PRIMARY KEY REFERENCES accounts ON DELETE RESTRICT,
    -- PHC string, e.g. $argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>.
    password_hash text NOT NULL CHECK (btrim(password_hash) <> ''),
    changed_at timestamptz NOT NULL
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts ON DELETE RESTRICT,
    token_hash bytea NOT NULL CHECK (length(token_hash) = 32),
    created_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL CHECK (last_seen_at >= created_at),
    expires_at timestamptz NOT NULL CHECK (expires_at > last_seen_at),
    CONSTRAINT sessions_token_hash_key UNIQUE (token_hash)
);

CREATE INDEX sessions_account_idx ON sessions (account_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE account_tokens (
    token_hash bytea PRIMARY KEY CHECK (length(token_hash) = 32),
    account_id uuid NOT NULL REFERENCES accounts ON DELETE RESTRICT,
    purpose text NOT NULL CHECK (purpose IN ('confirm-email')),
    email text COLLATE "C" NOT NULL CHECK (email <> ''),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at)
);

CREATE INDEX account_tokens_account_idx ON account_tokens (account_id, purpose);
CREATE INDEX account_tokens_expires_idx ON account_tokens (expires_at);

CREATE TABLE outbox_mails (
    id uuid PRIMARY KEY,
    recipient text NOT NULL CHECK (recipient <> ''),
    subject text NOT NULL CHECK (btrim(subject) <> ''),
    body_text text NOT NULL CHECK (btrim(body_text) <> ''),
    body_html text CHECK (body_html <> ''),
    template text CHECK (template <> ''),
    created_at timestamptz NOT NULL,
    attempts integer NOT NULL CHECK (attempts >= 0),
    -- NULL once the mail is sent or delivery was given up.
    next_attempt_at timestamptz,
    sent_at timestamptz,
    last_error text CHECK (last_error <> ''),
    CONSTRAINT outbox_mails_sent_not_due CHECK (sent_at IS NULL OR next_attempt_at IS NULL)
);

-- The worker's queue (ListDue) and the mail list (List, newest first).
CREATE INDEX outbox_mails_due_idx ON outbox_mails (next_attempt_at, id)
    WHERE sent_at IS NULL AND next_attempt_at IS NOT NULL;
CREATE INDEX outbox_mails_created_idx ON outbox_mails (created_at DESC, id DESC);

-- +goose Down

DROP TABLE outbox_mails;
DROP TABLE account_tokens;
DROP TABLE sessions;
DROP TABLE account_credentials;
ALTER TABLE accounts DROP COLUMN confirmed_at;
