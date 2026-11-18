-- +goose Up

CREATE TABLE push_subscriptions (
    id BINARY(16) PRIMARY KEY,
    account_id BINARY(16) NOT NULL,
    -- The store accepts HTTPS endpoints of up to 2048 bytes. The full
    -- endpoint can be indexed and compared bytewise at that length.
    endpoint VARBINARY(2048) NOT NULL,
    p256dh VARBINARY(65) NOT NULL,
    auth VARBINARY(16) NOT NULL,
    device_label LONGTEXT,
    created_at DATETIME(6) NOT NULL,
    last_used_at DATETIME(6) NOT NULL,
    CONSTRAINT push_subscriptions_endpoint_check CHECK (endpoint LIKE 'https://%' AND OCTET_LENGTH(endpoint) <= 2048),
    CONSTRAINT push_subscriptions_p256dh_check CHECK (OCTET_LENGTH(p256dh) = 65),
    CONSTRAINT push_subscriptions_auth_check CHECK (OCTET_LENGTH(auth) = 16),
    CONSTRAINT push_subscriptions_device_label_check CHECK (CHAR_LENGTH(device_label) <= 100),
    CONSTRAINT push_subscriptions_used_after_created CHECK (last_used_at >= created_at),
    UNIQUE KEY push_subscriptions_endpoint_key (endpoint),
    KEY push_subscriptions_account_idx (account_id, created_at, id),
    KEY push_subscriptions_last_used_idx (last_used_at),
    CONSTRAINT push_subscriptions_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE push_outbox (
    id BINARY(16) PRIMARY KEY,
    subscription_id BINARY(16) NOT NULL,
    -- A text column preserves exactly the JSON written by the caller.
    payload LONGTEXT NOT NULL,
    created_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    next_attempt_at DATETIME(6),
    sent_at DATETIME(6),
    failed_at DATETIME(6),
    last_error LONGTEXT,
    CONSTRAINT push_outbox_payload_check CHECK (JSON_VALID(payload) AND JSON_TYPE(payload) = 'OBJECT'),
    CONSTRAINT push_outbox_attempts_check CHECK (attempts >= 0),
    CONSTRAINT push_outbox_expires_after_created CHECK (expires_at > created_at),
    CONSTRAINT push_outbox_sent_or_failed CHECK (sent_at IS NULL OR failed_at IS NULL),
    CONSTRAINT push_outbox_pending_due CHECK (
        (sent_at IS NULL AND failed_at IS NULL) = (next_attempt_at IS NOT NULL)
    ),
    KEY push_outbox_due_idx (next_attempt_at, id),
    KEY push_outbox_subscription_idx (subscription_id),
    KEY push_outbox_created_idx (created_at),
    CONSTRAINT push_outbox_subscription_id_fkey
        FOREIGN KEY (subscription_id) REFERENCES push_subscriptions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down

DROP TABLE push_outbox;
DROP TABLE push_subscriptions;
