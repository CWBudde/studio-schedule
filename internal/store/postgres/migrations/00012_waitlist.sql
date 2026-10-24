-- The waiting list (ADR 0010): entries of persons for full lessons, their
-- invites, and how long an invite holds its place.
-- Conventions as in 00001_core.sql, except that entries go with their lesson
-- and their person: a lesson or person that may be deleted takes its entries
-- along (the services withdraw the open ones first).

-- +goose Up

ALTER TABLE settings
    ADD COLUMN waitlist_invite_expiry interval NOT NULL DEFAULT '12 hours'
        CHECK (waitlist_invite_expiry BETWEEN '1 minute' AND '30 days');

CREATE TABLE waitlist_entries (
    id uuid PRIMARY KEY,
    lesson_id uuid NOT NULL REFERENCES lessons ON DELETE CASCADE,
    person_id uuid NOT NULL REFERENCES persons ON DELETE CASCADE,
    status text NOT NULL CHECK (status IN ('waiting', 'invited', 'accepted', 'expired', 'withdrawn')),
    joined_at timestamptz NOT NULL,
    stay_on_list boolean NOT NULL,
    -- NULL if unknown; the account that put the person on the list.
    joined_by uuid REFERENCES accounts ON DELETE RESTRICT,
    -- The invite: NULL until the first one, kept when the entry ends.
    invite_token_hash bytea CHECK (octet_length(invite_token_hash) = 32),
    invited_at timestamptz,
    expires_at timestamptz,
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
    CONSTRAINT waitlist_entries_invited_after_join CHECK (invited_at >= joined_at)
);

-- A person is on a lesson's list at most once at a time.
CREATE UNIQUE INDEX waitlist_entries_one_open_key ON waitlist_entries (lesson_id, person_id)
    WHERE status IN ('waiting', 'invited');
CREATE UNIQUE INDEX waitlist_entries_token_key ON waitlist_entries (invite_token_hash);
CREATE INDEX waitlist_entries_lesson_idx ON waitlist_entries (lesson_id, status, joined_at, id);
CREATE INDEX waitlist_entries_person_idx ON waitlist_entries (person_id, joined_at, id);
CREATE INDEX waitlist_entries_joined_by_idx ON waitlist_entries (joined_by);
-- The expiry job looks for invites that ran out.
CREATE INDEX waitlist_entries_expiry_idx ON waitlist_entries (expires_at) WHERE status = 'invited';

-- +goose Down

DROP TABLE waitlist_entries;

ALTER TABLE settings DROP COLUMN waitlist_invite_expiry;
