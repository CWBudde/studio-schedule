-- The waiting list (ADR 0010): entries of persons for full lessons, their
-- invites, and how long an invite holds its place.
-- Conventions as in 00001_core.sql, except that entries go with their lesson
-- and their person: a lesson or person that may be deleted takes its entries
-- along (the services withdraw the open ones first).

-- +goose Up

ALTER TABLE settings
    ADD COLUMN waitlist_invite_expiry BIGINT NOT NULL DEFAULT 43200000000,
    ADD CONSTRAINT settings_waitlist_invite_expiry_check CHECK (waitlist_invite_expiry BETWEEN 60000000 AND 2592000000000);

CREATE TABLE waitlist_entries (
    id BINARY(16) PRIMARY KEY,
    lesson_id BINARY(16) NOT NULL,
    person_id BINARY(16) NOT NULL,
    status VARBINARY(32) NOT NULL,
    joined_at DATETIME(6) NOT NULL,
    stay_on_list BOOLEAN NOT NULL,
    -- NULL if unknown; the account that put the person on the list.
    joined_by BINARY(16),
    -- The invite: NULL until the first one, kept when the entry ends.
    invite_token_hash VARBINARY(32),
    invited_at DATETIME(6),
    expires_at DATETIME(6),
    -- Closed entries generate NULL and are exempt from uniqueness.
    open_person_id BINARY(16) GENERATED ALWAYS AS (
        CASE WHEN status IN ('waiting', 'invited') THEN person_id END
    ) VIRTUAL,
    -- An entry back on the list after an invite keeps the old link's hash,
    -- without its window, until a new invite replaces it.
    CONSTRAINT waitlist_entries_invite_complete CHECK (
        (invited_at IS NULL AND expires_at IS NULL)
        OR (invite_token_hash IS NOT NULL AND invited_at IS NOT NULL AND expires_at > invited_at)
    ),
    CONSTRAINT waitlist_entries_waiting_uninvited CHECK (
        status <> 'waiting' OR (invited_at IS NULL AND expires_at IS NULL)
    ),
    -- Only a waiting entry has a hash without its invite.
    CONSTRAINT waitlist_entries_hash_with_invite CHECK (
        status = 'waiting' OR invite_token_hash IS NULL OR invited_at IS NOT NULL
    ),
    CONSTRAINT waitlist_entries_invited_has_invite CHECK (
        status NOT IN ('invited', 'accepted') OR invite_token_hash IS NOT NULL
    ),
    CONSTRAINT waitlist_entries_invited_after_join CHECK (invited_at >= joined_at),
    CONSTRAINT waitlist_entries_status_check CHECK (status IN ('waiting', 'invited', 'accepted', 'expired', 'withdrawn')),
    CONSTRAINT waitlist_entries_invite_token_hash_check CHECK (OCTET_LENGTH(invite_token_hash) = 32),
    UNIQUE KEY waitlist_entries_one_open_key (lesson_id, open_person_id),
    UNIQUE KEY waitlist_entries_token_key (invite_token_hash),
    KEY waitlist_entries_lesson_idx (lesson_id, status, joined_at, id),
    KEY waitlist_entries_person_idx (person_id, joined_at, id),
    KEY waitlist_entries_joined_by_idx (joined_by),
    KEY waitlist_entries_expiry_idx (expires_at),
    CONSTRAINT waitlist_entries_lesson_id_fkey FOREIGN KEY (lesson_id) REFERENCES lessons(id) ON DELETE CASCADE,
    CONSTRAINT waitlist_entries_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id) ON DELETE CASCADE,
    CONSTRAINT waitlist_entries_joined_by_fkey FOREIGN KEY (joined_by) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down

DROP TABLE waitlist_entries;

ALTER TABLE settings DROP CONSTRAINT settings_waitlist_invite_expiry_check, DROP COLUMN waitlist_invite_expiry;
