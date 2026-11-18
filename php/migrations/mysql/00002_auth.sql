-- Authentication and mail: confirmed accounts, passwords, sessions,
-- single-use email tokens and the mail outbox.
--
-- Secret tokens (session cookies, email links) are stored as their SHA-256
-- hash only (domain.HashToken), so a leaked database holds no usable token.
-- Conventions as in 00001_core.sql.

-- +goose Up

ALTER TABLE accounts ADD COLUMN confirmed_at DATETIME(6);

-- Accounts from before registration existed were created by the school (seed
-- or import), so their addresses count as confirmed.
UPDATE accounts SET confirmed_at = UTC_TIMESTAMP(6) WHERE confirmed_at IS NULL;

CREATE TABLE account_credentials (
    account_id BINARY(16) PRIMARY KEY,
    -- PHC string, e.g. $argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>.
    password_hash LONGTEXT NOT NULL,
    changed_at DATETIME(6) NOT NULL,
    CONSTRAINT account_credentials_password_hash_check CHECK (OCTET_LENGTH(TRIM(password_hash)) > 0),
    CONSTRAINT account_credentials_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE sessions (
    id BINARY(16) PRIMARY KEY,
    account_id BINARY(16) NOT NULL,
    token_hash VARBINARY(32) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    last_seen_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    CONSTRAINT sessions_token_hash_key UNIQUE (token_hash),
    CONSTRAINT sessions_token_hash_check CHECK (OCTET_LENGTH(token_hash) = 32),
    CONSTRAINT sessions_last_seen_at_check CHECK (last_seen_at >= created_at),
    CONSTRAINT sessions_expires_at_check CHECK (expires_at > last_seen_at),
    KEY sessions_account_idx (account_id),
    KEY sessions_expires_idx (expires_at),
    CONSTRAINT sessions_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE account_tokens (
    token_hash VARBINARY(32) PRIMARY KEY,
    account_id BINARY(16) NOT NULL,
    purpose VARBINARY(32) NOT NULL,
    email LONGTEXT NOT NULL,
    created_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    CONSTRAINT account_tokens_token_hash_check CHECK (OCTET_LENGTH(token_hash) = 32),
    CONSTRAINT account_tokens_purpose_check CHECK (purpose IN ('confirm-email')),
    CONSTRAINT account_tokens_email_check CHECK (OCTET_LENGTH(email) > 0),
    CONSTRAINT account_tokens_expires_at_check CHECK (expires_at > created_at),
    KEY account_tokens_account_idx (account_id, purpose),
    KEY account_tokens_expires_idx (expires_at),
    CONSTRAINT account_tokens_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE outbox_mails (
    id BINARY(16) PRIMARY KEY,
    recipient LONGTEXT NOT NULL,
    subject LONGTEXT NOT NULL,
    body_text LONGTEXT NOT NULL,
    body_html LONGTEXT,
    template VARBINARY(255),
    created_at DATETIME(6) NOT NULL,
    attempts INT NOT NULL,
    -- NULL once the mail is sent or delivery was given up.
    next_attempt_at DATETIME(6),
    sent_at DATETIME(6),
    last_error LONGTEXT,
    CONSTRAINT outbox_mails_sent_not_due CHECK (sent_at IS NULL OR next_attempt_at IS NULL),
    CONSTRAINT outbox_mails_recipient_check CHECK (OCTET_LENGTH(recipient) > 0),
    CONSTRAINT outbox_mails_subject_check CHECK (OCTET_LENGTH(TRIM(subject)) > 0),
    CONSTRAINT outbox_mails_body_text_check CHECK (OCTET_LENGTH(TRIM(body_text)) > 0),
    CONSTRAINT outbox_mails_body_html_check CHECK (OCTET_LENGTH(body_html) > 0),
    CONSTRAINT outbox_mails_template_check CHECK (OCTET_LENGTH(template) > 0),
    CONSTRAINT outbox_mails_attempts_check CHECK (attempts >= 0),
    CONSTRAINT outbox_mails_last_error_check CHECK (OCTET_LENGTH(last_error) > 0),
    KEY outbox_mails_due_idx (next_attempt_at, id),
    KEY outbox_mails_created_idx (created_at DESC, id DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down

DROP TABLE outbox_mails;
DROP TABLE account_tokens;
DROP TABLE sessions;
DROP TABLE account_credentials;
ALTER TABLE accounts DROP COLUMN confirmed_at;
