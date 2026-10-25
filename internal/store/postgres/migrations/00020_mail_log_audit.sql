-- The mail log and the wider audit log (ADR 0014). Conventions as in
-- 00001_core.sql.
--
-- Outbox: the purge removes the bodies of mails sent more than 30 days ago
-- (body_text and body_html NULL), so a sent mail may have no body; the log
-- filters by template and finds the mails to purge by their sending time.
--
-- Audit: an event targets an account, a person or nothing (target_kind),
-- and records what else matters as a JSON object (details). The actions
-- are rows of audit_actions instead of a CHECK list, so that a new action
-- is a single INSERT.

-- +goose Up

ALTER TABLE outbox_mails ALTER COLUMN body_text DROP NOT NULL,
    DROP CONSTRAINT outbox_mails_body_text_check,
    ADD CONSTRAINT outbox_mails_body_text_check CHECK (
        btrim(body_text) <> '' OR (body_text IS NULL AND body_html IS NULL AND sent_at IS NOT NULL)
    );

CREATE INDEX outbox_mails_template_idx ON outbox_mails (template, created_at DESC, id DESC);
CREATE INDEX outbox_mails_purge_idx ON outbox_mails (sent_at) WHERE body_text IS NOT NULL AND sent_at IS NOT NULL;

CREATE TABLE audit_actions (
    action text COLLATE "C" PRIMARY KEY
);

INSERT INTO audit_actions (action) VALUES ('impersonation-started'), ('impersonation-ended'), ('mail-requeued');

ALTER TABLE audit_events DROP CONSTRAINT audit_events_action_check,
    ADD CONSTRAINT audit_events_action_fkey FOREIGN KEY (action) REFERENCES audit_actions ON DELETE RESTRICT,
    ALTER COLUMN target_id DROP NOT NULL,
    ADD COLUMN target_kind text CHECK (target_kind IN ('account', 'person')),
    ADD COLUMN target_person_id uuid REFERENCES persons ON DELETE RESTRICT,
    ADD COLUMN details jsonb CHECK (jsonb_typeof(details) = 'object' AND details <> '{}');

UPDATE audit_events SET target_kind = 'account';

ALTER TABLE audit_events ADD CONSTRAINT audit_events_target CHECK (
    (target_kind IS NOT DISTINCT FROM 'account') = (target_id IS NOT NULL)
    AND (target_kind IS NOT DISTINCT FROM 'person') = (target_person_id IS NOT NULL)
);

CREATE INDEX audit_events_target_person_idx ON audit_events (target_person_id) WHERE target_person_id IS NOT NULL;

-- +goose Down

DELETE FROM audit_events WHERE target_kind IS DISTINCT FROM 'account';

DROP INDEX audit_events_target_person_idx;
ALTER TABLE audit_events DROP CONSTRAINT audit_events_target,
    DROP COLUMN details,
    DROP COLUMN target_person_id,
    DROP COLUMN target_kind,
    ALTER COLUMN target_id SET NOT NULL,
    DROP CONSTRAINT audit_events_action_fkey,
    ADD CONSTRAINT audit_events_action_check CHECK (action IN ('impersonation-started', 'impersonation-ended'));

DROP TABLE audit_actions;

DROP INDEX outbox_mails_purge_idx;
DROP INDEX outbox_mails_template_idx;

DELETE FROM outbox_mails WHERE body_text IS NULL;
ALTER TABLE outbox_mails DROP CONSTRAINT outbox_mails_body_text_check,
    ADD CONSTRAINT outbox_mails_body_text_check CHECK (btrim(body_text) <> ''),
    ALTER COLUMN body_text SET NOT NULL;
