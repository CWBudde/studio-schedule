-- +goose Up

ALTER TABLE outbox_mails
    DROP CONSTRAINT outbox_mails_body_text_check,
    MODIFY COLUMN body_text LONGTEXT,
    ADD CONSTRAINT outbox_mails_body_text_check CHECK (
        OCTET_LENGTH(TRIM(body_text)) > 0 OR (body_text IS NULL AND body_html IS NULL AND sent_at IS NOT NULL)
    ),
    ADD KEY outbox_mails_template_idx (template, created_at DESC, id DESC),
    ADD KEY outbox_mails_purge_idx (sent_at);

CREATE TABLE audit_actions (
    action VARBINARY(32) PRIMARY KEY
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

INSERT INTO audit_actions (action) VALUES ('impersonation-started'), ('impersonation-ended'), ('mail-requeued');

ALTER TABLE audit_events
    DROP CONSTRAINT audit_events_action_check,
    MODIFY COLUMN target_id BINARY(16),
    ADD COLUMN target_kind VARBINARY(32),
    ADD COLUMN target_person_id BINARY(16),
    ADD COLUMN details JSON,
    ADD CONSTRAINT audit_events_target_kind_check CHECK (target_kind IN ('account', 'person')),
    ADD CONSTRAINT audit_events_details_check CHECK (JSON_TYPE(details) = 'OBJECT' AND JSON_LENGTH(details) > 0),
    ADD KEY audit_events_action_idx (action),
    ADD KEY audit_events_target_person_idx (target_person_id),
    ADD CONSTRAINT audit_events_action_fkey FOREIGN KEY (action) REFERENCES audit_actions(action) ON DELETE RESTRICT,
    ADD CONSTRAINT audit_events_target_person_id_fkey
        FOREIGN KEY (target_person_id) REFERENCES persons(id) ON DELETE RESTRICT;

UPDATE audit_events SET target_kind = 'account';

ALTER TABLE audit_events ADD CONSTRAINT audit_events_target CHECK (
    (target_kind <=> 'account') = (target_id IS NOT NULL)
    AND (target_kind <=> 'person') = (target_person_id IS NOT NULL)
);

-- +goose Down

DELETE FROM audit_events WHERE NOT (target_kind <=> 'account');

ALTER TABLE audit_events
    DROP FOREIGN KEY audit_events_action_fkey,
    DROP FOREIGN KEY audit_events_target_id_fkey,
    DROP FOREIGN KEY audit_events_target_person_id_fkey;
ALTER TABLE audit_events
    DROP CONSTRAINT audit_events_target,
    DROP CONSTRAINT audit_events_details_check,
    DROP CONSTRAINT audit_events_target_kind_check,
    DROP INDEX audit_events_target_person_idx,
    DROP INDEX audit_events_action_idx,
    DROP COLUMN details,
    DROP COLUMN target_person_id,
    DROP COLUMN target_kind,
    MODIFY COLUMN target_id BINARY(16) NOT NULL,
    ADD CONSTRAINT audit_events_target_id_fkey FOREIGN KEY (target_id) REFERENCES accounts(id) ON DELETE RESTRICT,
    ADD CONSTRAINT audit_events_action_check CHECK (action IN ('impersonation-started', 'impersonation-ended'));

DROP TABLE audit_actions;

DELETE FROM outbox_mails WHERE body_text IS NULL;
ALTER TABLE outbox_mails
    DROP INDEX outbox_mails_purge_idx,
    DROP INDEX outbox_mails_template_idx,
    DROP CONSTRAINT outbox_mails_body_text_check,
    MODIFY COLUMN body_text LONGTEXT NOT NULL,
    ADD CONSTRAINT outbox_mails_body_text_check CHECK (OCTET_LENGTH(TRIM(body_text)) > 0);
