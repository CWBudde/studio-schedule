-- Password reset and email change: account tokens get the purposes
-- 'reset-password' and 'change-email' (domain.TokenPurposes).
-- Conventions as in 00001_core.sql.

-- +goose Up

ALTER TABLE account_tokens DROP CONSTRAINT account_tokens_purpose_check,
    ADD CONSTRAINT account_tokens_purpose_check
        CHECK (purpose IN ('confirm-email', 'reset-password', 'change-email'));

-- +goose Down

DELETE FROM account_tokens WHERE purpose IN ('reset-password', 'change-email');

ALTER TABLE account_tokens DROP CONSTRAINT account_tokens_purpose_check,
    ADD CONSTRAINT account_tokens_purpose_check CHECK (purpose IN ('confirm-email'));
