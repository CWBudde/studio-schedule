-- +goose Up

ALTER TABLE persons
    ADD COLUMN anonymized_at DATETIME(6),
    ADD CONSTRAINT persons_anonymized_no_data CHECK (
        anonymized_at IS NULL OR (
            email IS NULL AND phone IS NULL AND birth_date IS NULL AND notes IS NULL AND member_no IS NULL
        )
    );

ALTER TABLE account_credentials DROP FOREIGN KEY account_credentials_account_id_fkey;
ALTER TABLE account_credentials
    ADD CONSTRAINT account_credentials_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE sessions
    DROP FOREIGN KEY sessions_account_id_fkey,
    DROP FOREIGN KEY sessions_impersonator_id_fkey;
ALTER TABLE sessions
    ADD CONSTRAINT sessions_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    ADD CONSTRAINT sessions_impersonator_id_fkey FOREIGN KEY (impersonator_id) REFERENCES accounts(id) ON DELETE CASCADE;
ALTER TABLE account_tokens DROP FOREIGN KEY account_tokens_account_id_fkey;
ALTER TABLE account_tokens
    ADD CONSTRAINT account_tokens_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE;

ALTER TABLE booking_events DROP FOREIGN KEY booking_events_actor_account_id_fkey;
ALTER TABLE booking_events
    ADD CONSTRAINT booking_events_actor_account_id_fkey
        FOREIGN KEY (actor_account_id) REFERENCES accounts(id) ON DELETE SET NULL;
ALTER TABLE waitlist_entries DROP FOREIGN KEY waitlist_entries_joined_by_fkey;
ALTER TABLE waitlist_entries
    ADD CONSTRAINT waitlist_entries_joined_by_fkey FOREIGN KEY (joined_by) REFERENCES accounts(id) ON DELETE SET NULL;

-- MySQL forbids referring to a column with ON DELETE SET NULL in a CHECK.
-- Row triggers handle that part of the rule. The FK can only clear canceled_by.
ALTER TABLE lessons
    DROP FOREIGN KEY lessons_canceled_by_fkey,
    DROP CONSTRAINT lessons_cancellation_when_canceled,
    ADD CONSTRAINT lessons_cancellation_when_canceled CHECK (
        status = 'canceled' OR (cancel_note IS NULL AND canceled_at IS NULL)
    );
ALTER TABLE lessons
    ADD CONSTRAINT lessons_canceled_by_fkey FOREIGN KEY (canceled_by) REFERENCES accounts(id) ON DELETE SET NULL;

-- +goose StatementBegin
CREATE TRIGGER lessons_cancellation_actor_insert BEFORE INSERT ON lessons
FOR EACH ROW
BEGIN
    IF NEW.status <> 'canceled' AND NEW.canceled_by IS NOT NULL THEN
        SIGNAL SQLSTATE '23000' SET MESSAGE_TEXT = 'Only canceled lessons can have a cancellation actor';
    END IF;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER lessons_cancellation_actor_update BEFORE UPDATE ON lessons
FOR EACH ROW
BEGIN
    IF NEW.status <> 'canceled' AND NEW.canceled_by IS NOT NULL THEN
        SIGNAL SQLSTATE '23000' SET MESSAGE_TEXT = 'Only canceled lessons can have a cancellation actor';
    END IF;
END;
-- +goose StatementEnd

ALTER TABLE passes DROP FOREIGN KEY passes_sold_by_fkey;
ALTER TABLE passes
    ADD CONSTRAINT passes_sold_by_fkey FOREIGN KEY (sold_by) REFERENCES accounts(id) ON DELETE SET NULL;
ALTER TABLE import_runs DROP FOREIGN KEY import_runs_actor_id_fkey;
ALTER TABLE import_runs
    ADD CONSTRAINT import_runs_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES accounts(id) ON DELETE SET NULL;
ALTER TABLE lesson_notices DROP FOREIGN KEY lesson_notices_sender_id_fkey;
ALTER TABLE lesson_notices
    MODIFY COLUMN sender_id BINARY(16),
    ADD CONSTRAINT lesson_notices_sender_id_fkey FOREIGN KEY (sender_id) REFERENCES accounts(id) ON DELETE SET NULL;

ALTER TABLE audit_events
    MODIFY COLUMN actor_id BINARY(16),
    ADD CONSTRAINT audit_events_actor CHECK (actor_id IS NOT NULL OR action = 'account-deleted');

-- InnoDB already indexes each of these foreign keys.
ALTER TABLE passes RENAME INDEX passes_sold_by_fkey TO passes_sold_by_idx;
ALTER TABLE import_runs RENAME INDEX import_runs_actor_id_fkey TO import_runs_actor_idx;
ALTER TABLE receipts RENAME INDEX receipts_created_by_fkey TO receipts_created_by_idx;
ALTER TABLE person_duplicate_dismissals
    RENAME INDEX person_duplicate_dismissals_dismissed_by_fkey TO person_duplicate_dismissals_dismissed_by_idx;
CREATE INDEX bookings_guest_idx ON bookings (lesson_id);

INSERT INTO audit_actions (action) VALUES ('person-anonymized'), ('account-disabled'), ('account-enabled'),
    ('account-role-changed'), ('account-deleted');

-- +goose Down

DELETE FROM audit_events WHERE action IN (
    'person-anonymized', 'account-disabled', 'account-enabled', 'account-role-changed', 'account-deleted'
);
DELETE FROM audit_actions WHERE action IN (
    'person-anonymized', 'account-disabled', 'account-enabled', 'account-role-changed', 'account-deleted'
);

DROP INDEX bookings_guest_idx ON bookings;
ALTER TABLE person_duplicate_dismissals
    RENAME INDEX person_duplicate_dismissals_dismissed_by_idx TO person_duplicate_dismissals_dismissed_by_fkey;
ALTER TABLE receipts RENAME INDEX receipts_created_by_idx TO receipts_created_by_fkey;
ALTER TABLE import_runs RENAME INDEX import_runs_actor_idx TO import_runs_actor_id_fkey;
ALTER TABLE passes RENAME INDEX passes_sold_by_idx TO passes_sold_by_fkey;

ALTER TABLE audit_events DROP FOREIGN KEY audit_events_actor_id_fkey;
ALTER TABLE audit_events
    DROP CONSTRAINT audit_events_actor,
    MODIFY COLUMN actor_id BINARY(16) NOT NULL,
    ADD CONSTRAINT audit_events_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES accounts(id) ON DELETE RESTRICT;

-- The notices of deleted accounts cannot keep a sender.
DELETE FROM lesson_notices WHERE sender_id IS NULL;
ALTER TABLE lesson_notices DROP FOREIGN KEY lesson_notices_sender_id_fkey;
ALTER TABLE lesson_notices
    MODIFY COLUMN sender_id BINARY(16) NOT NULL,
    ADD CONSTRAINT lesson_notices_sender_id_fkey FOREIGN KEY (sender_id) REFERENCES accounts(id) ON DELETE RESTRICT;
ALTER TABLE import_runs DROP FOREIGN KEY import_runs_actor_id_fkey;
ALTER TABLE import_runs
    ADD CONSTRAINT import_runs_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES accounts(id) ON DELETE RESTRICT;
ALTER TABLE passes DROP FOREIGN KEY passes_sold_by_fkey;
ALTER TABLE passes
    ADD CONSTRAINT passes_sold_by_fkey FOREIGN KEY (sold_by) REFERENCES accounts(id) ON DELETE RESTRICT;

ALTER TABLE lessons DROP FOREIGN KEY lessons_canceled_by_fkey;
ALTER TABLE lessons
    ADD CONSTRAINT lessons_canceled_by_fkey FOREIGN KEY (canceled_by) REFERENCES accounts(id) ON DELETE RESTRICT,
    DROP CONSTRAINT lessons_cancellation_when_canceled,
    ADD CONSTRAINT lessons_cancellation_when_canceled CHECK (
        status = 'canceled' OR (cancel_note IS NULL AND canceled_at IS NULL AND canceled_by IS NULL)
    );
DROP TRIGGER lessons_cancellation_actor_update;
DROP TRIGGER lessons_cancellation_actor_insert;

ALTER TABLE waitlist_entries DROP FOREIGN KEY waitlist_entries_joined_by_fkey;
ALTER TABLE waitlist_entries
    ADD CONSTRAINT waitlist_entries_joined_by_fkey FOREIGN KEY (joined_by) REFERENCES accounts(id) ON DELETE RESTRICT;
ALTER TABLE booking_events DROP FOREIGN KEY booking_events_actor_account_id_fkey;
ALTER TABLE booking_events
    ADD CONSTRAINT booking_events_actor_account_id_fkey
        FOREIGN KEY (actor_account_id) REFERENCES accounts(id) ON DELETE RESTRICT;

ALTER TABLE account_tokens DROP FOREIGN KEY account_tokens_account_id_fkey;
ALTER TABLE account_tokens
    ADD CONSTRAINT account_tokens_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE RESTRICT;
ALTER TABLE sessions
    DROP FOREIGN KEY sessions_impersonator_id_fkey,
    DROP FOREIGN KEY sessions_account_id_fkey;
ALTER TABLE sessions
    ADD CONSTRAINT sessions_impersonator_id_fkey FOREIGN KEY (impersonator_id) REFERENCES accounts(id) ON DELETE RESTRICT,
    ADD CONSTRAINT sessions_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE RESTRICT;
ALTER TABLE account_credentials DROP FOREIGN KEY account_credentials_account_id_fkey;
ALTER TABLE account_credentials
    ADD CONSTRAINT account_credentials_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE RESTRICT;

ALTER TABLE persons
    DROP CONSTRAINT persons_anonymized_no_data,
    DROP COLUMN anonymized_at;
