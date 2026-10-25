-- Privacy and account administration (ADR 0016). Conventions as in
-- 00001_core.sql, except for the foreign keys to accounts changed here.
--
-- Persons: anonymized_at marks a person the office anonymized; such a person
-- has the fixed name and no other personal data, and stays for its history.
--
-- Deleting an account (by an admin, or by its holder) deletes what is its
-- own: its password, sessions (and the impersonation sessions it started)
-- and email tokens (CASCADE; calendar feeds and announcement dismissals
-- cascaded already). Records that only name it as who did something forget
-- it (SET NULL): booking events, waiting-list entries it added, lessons it
-- canceled, passes it sold, import runs and lesson notices. The store
-- re-points the audit events that target the account to its person before
-- it deletes it. Receipts it created, audit events it acted in,
-- announcements it wrote or mailed and duplicate dismissals it recorded keep
-- it (RESTRICT): such an account is disabled, not deleted.
--
-- audit_events.actor_id: NULL only for an account its holder deleted
-- (account-deleted).

-- +goose Up

ALTER TABLE persons ADD COLUMN anonymized_at timestamptz,
    ADD CONSTRAINT persons_anonymized_no_data CHECK (
        anonymized_at IS NULL OR (
            email IS NULL AND phone IS NULL AND birth_date IS NULL AND notes IS NULL AND member_no IS NULL
        )
    );

ALTER TABLE account_credentials DROP CONSTRAINT account_credentials_account_id_fkey,
    ADD CONSTRAINT account_credentials_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts ON DELETE CASCADE;
ALTER TABLE sessions DROP CONSTRAINT sessions_account_id_fkey,
    ADD CONSTRAINT sessions_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts ON DELETE CASCADE,
    DROP CONSTRAINT sessions_impersonator_id_fkey,
    ADD CONSTRAINT sessions_impersonator_id_fkey FOREIGN KEY (impersonator_id) REFERENCES accounts ON DELETE CASCADE;
ALTER TABLE account_tokens DROP CONSTRAINT account_tokens_account_id_fkey,
    ADD CONSTRAINT account_tokens_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts ON DELETE CASCADE;

ALTER TABLE booking_events DROP CONSTRAINT booking_events_actor_account_id_fkey,
    ADD CONSTRAINT booking_events_actor_account_id_fkey FOREIGN KEY (actor_account_id)
        REFERENCES accounts ON DELETE SET NULL;
ALTER TABLE waitlist_entries DROP CONSTRAINT waitlist_entries_joined_by_fkey,
    ADD CONSTRAINT waitlist_entries_joined_by_fkey FOREIGN KEY (joined_by) REFERENCES accounts ON DELETE SET NULL;
ALTER TABLE lessons DROP CONSTRAINT lessons_canceled_by_fkey,
    ADD CONSTRAINT lessons_canceled_by_fkey FOREIGN KEY (canceled_by) REFERENCES accounts ON DELETE SET NULL;
ALTER TABLE passes DROP CONSTRAINT passes_sold_by_fkey,
    ADD CONSTRAINT passes_sold_by_fkey FOREIGN KEY (sold_by) REFERENCES accounts ON DELETE SET NULL;
ALTER TABLE import_runs DROP CONSTRAINT import_runs_actor_id_fkey,
    ADD CONSTRAINT import_runs_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES accounts ON DELETE SET NULL;
ALTER TABLE lesson_notices ALTER COLUMN sender_id DROP NOT NULL,
    DROP CONSTRAINT lesson_notices_sender_id_fkey,
    ADD CONSTRAINT lesson_notices_sender_id_fkey FOREIGN KEY (sender_id) REFERENCES accounts ON DELETE SET NULL;

ALTER TABLE audit_events ALTER COLUMN actor_id DROP NOT NULL,
    ADD CONSTRAINT audit_events_actor CHECK (actor_id IS NOT NULL OR action = 'account-deleted');

-- The foreign keys an account deletion checks or changes, and the guest
-- bookings the retention job anonymizes.
CREATE INDEX passes_sold_by_idx ON passes (sold_by) WHERE sold_by IS NOT NULL;
CREATE INDEX import_runs_actor_idx ON import_runs (actor_id) WHERE actor_id IS NOT NULL;
CREATE INDEX receipts_created_by_idx ON receipts (created_by) WHERE created_by IS NOT NULL;
CREATE INDEX person_duplicate_dismissals_dismissed_by_idx ON person_duplicate_dismissals (dismissed_by);
CREATE INDEX bookings_guest_idx ON bookings (lesson_id) WHERE person_id IS NULL;

INSERT INTO audit_actions (action) VALUES ('person-anonymized'), ('account-disabled'), ('account-enabled'),
    ('account-role-changed'), ('account-deleted');

-- +goose Down

DELETE FROM audit_events WHERE action IN (
    'person-anonymized', 'account-disabled', 'account-enabled', 'account-role-changed', 'account-deleted'
);
DELETE FROM audit_actions WHERE action IN (
    'person-anonymized', 'account-disabled', 'account-enabled', 'account-role-changed', 'account-deleted'
);

DROP INDEX bookings_guest_idx;
DROP INDEX person_duplicate_dismissals_dismissed_by_idx;
DROP INDEX receipts_created_by_idx;
DROP INDEX import_runs_actor_idx;
DROP INDEX passes_sold_by_idx;

ALTER TABLE audit_events DROP CONSTRAINT audit_events_actor,
    ALTER COLUMN actor_id SET NOT NULL;

-- The notices of deleted accounts cannot keep a sender.
DELETE FROM lesson_notices WHERE sender_id IS NULL;
ALTER TABLE lesson_notices DROP CONSTRAINT lesson_notices_sender_id_fkey,
    ADD CONSTRAINT lesson_notices_sender_id_fkey FOREIGN KEY (sender_id) REFERENCES accounts ON DELETE RESTRICT,
    ALTER COLUMN sender_id SET NOT NULL;
ALTER TABLE import_runs DROP CONSTRAINT import_runs_actor_id_fkey,
    ADD CONSTRAINT import_runs_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES accounts ON DELETE RESTRICT;
ALTER TABLE passes DROP CONSTRAINT passes_sold_by_fkey,
    ADD CONSTRAINT passes_sold_by_fkey FOREIGN KEY (sold_by) REFERENCES accounts ON DELETE RESTRICT;
ALTER TABLE lessons DROP CONSTRAINT lessons_canceled_by_fkey,
    ADD CONSTRAINT lessons_canceled_by_fkey FOREIGN KEY (canceled_by) REFERENCES accounts ON DELETE RESTRICT;
ALTER TABLE waitlist_entries DROP CONSTRAINT waitlist_entries_joined_by_fkey,
    ADD CONSTRAINT waitlist_entries_joined_by_fkey FOREIGN KEY (joined_by) REFERENCES accounts ON DELETE RESTRICT;
ALTER TABLE booking_events DROP CONSTRAINT booking_events_actor_account_id_fkey,
    ADD CONSTRAINT booking_events_actor_account_id_fkey FOREIGN KEY (actor_account_id)
        REFERENCES accounts ON DELETE RESTRICT;

ALTER TABLE account_tokens DROP CONSTRAINT account_tokens_account_id_fkey,
    ADD CONSTRAINT account_tokens_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts ON DELETE RESTRICT;
ALTER TABLE sessions DROP CONSTRAINT sessions_impersonator_id_fkey,
    ADD CONSTRAINT sessions_impersonator_id_fkey FOREIGN KEY (impersonator_id) REFERENCES accounts ON DELETE RESTRICT,
    DROP CONSTRAINT sessions_account_id_fkey,
    ADD CONSTRAINT sessions_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts ON DELETE RESTRICT;
ALTER TABLE account_credentials DROP CONSTRAINT account_credentials_account_id_fkey,
    ADD CONSTRAINT account_credentials_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts ON DELETE RESTRICT;

ALTER TABLE persons DROP CONSTRAINT persons_anonymized_no_data,
    DROP COLUMN anonymized_at;
