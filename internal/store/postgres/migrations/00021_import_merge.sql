-- The member import, duplicates and merges (ADR 0015). Conventions as in
-- 00001_core.sql.
--
-- Persons: a member number, unique, by which the import finds a person
-- again; and the tombstone of a merge, merged_into, which keeps the receipts
-- a merged person paid (receipts are immutable, ADR 0012). A tombstone has
-- no member number, so the person it was merged into can take it.
--
-- import_runs: the history of applied imports, with their counts and the
-- rows they could not take (a JSON array of {line, message}).
--
-- person_duplicate_dismissals: pairs the office found not to be the same
-- person, ordered by ID, so a pair has one key; they go with either person.
--
-- bookings.merged_from: a merge keeps the merged person's booking of a
-- lesson that had started beside the survivor's own booking of it, as
-- history; such a booking names the merged person and is left out of the
-- one-active-booking-per-person-and-lesson index.

-- +goose Up

ALTER TABLE persons
    ADD COLUMN member_no text COLLATE "C" CHECK (member_no <> '' AND member_no = btrim(member_no)),
    ADD COLUMN merged_into uuid REFERENCES persons ON DELETE RESTRICT,
    ADD CONSTRAINT persons_not_merged_into_self CHECK (merged_into <> id),
    ADD CONSTRAINT persons_tombstone_no_member_no CHECK (merged_into IS NULL OR member_no IS NULL);

CREATE UNIQUE INDEX persons_member_no_key ON persons (member_no);
CREATE INDEX persons_merged_into_idx ON persons (merged_into) WHERE merged_into IS NOT NULL;

CREATE TABLE import_runs (
    id uuid PRIMARY KEY,
    file_name text NOT NULL CHECK (btrim(file_name) <> ''),
    -- NULL: the command line.
    actor_id uuid REFERENCES accounts ON DELETE RESTRICT,
    started_at timestamptz NOT NULL,
    finished_at timestamptz NOT NULL CHECK (finished_at >= started_at),
    rows_total integer NOT NULL CHECK (rows_total >= 0),
    created integer NOT NULL CHECK (created >= 0),
    updated integer NOT NULL CHECK (updated >= 0),
    unchanged integer NOT NULL CHECK (unchanged >= 0),
    failed integer NOT NULL CHECK (failed >= 0),
    guardians_created integer NOT NULL CHECK (guardians_created >= 0),
    guardians_updated integer NOT NULL CHECK (guardians_updated >= 0),
    links_added integer NOT NULL CHECK (links_added >= 0),
    errors jsonb CHECK (jsonb_typeof(errors) = 'array' AND errors <> '[]'),
    CONSTRAINT import_runs_rows CHECK (rows_total = created + updated + unchanged + failed)
);

CREATE INDEX import_runs_started_idx ON import_runs (started_at DESC, id DESC);

CREATE TABLE person_duplicate_dismissals (
    person_a uuid NOT NULL REFERENCES persons ON DELETE CASCADE,
    person_b uuid NOT NULL REFERENCES persons ON DELETE CASCADE,
    dismissed_by uuid NOT NULL REFERENCES accounts ON DELETE RESTRICT,
    dismissed_at timestamptz NOT NULL,
    PRIMARY KEY (person_a, person_b),
    CONSTRAINT person_duplicate_dismissals_ordered CHECK (person_a < person_b)
);

CREATE INDEX person_duplicate_dismissals_person_b_idx ON person_duplicate_dismissals (person_b);

ALTER TABLE bookings
    ADD COLUMN merged_from uuid REFERENCES persons ON DELETE RESTRICT,
    ADD CONSTRAINT bookings_merged_from_other CHECK (merged_from <> person_id);

DROP INDEX bookings_one_active_key;
CREATE UNIQUE INDEX bookings_one_active_key ON bookings (lesson_id, person_id)
    WHERE status <> 'canceled' AND person_id IS NOT NULL AND merged_from IS NULL;
CREATE INDEX bookings_merged_from_idx ON bookings (merged_from) WHERE merged_from IS NOT NULL;

INSERT INTO audit_actions (action) VALUES ('person-merged');

-- +goose Down

DELETE FROM audit_events WHERE action = 'person-merged';
DELETE FROM audit_actions WHERE action = 'person-merged';

-- A booking kept as history goes back to the tombstone it came from, which
-- had one active booking of the lesson.
UPDATE bookings SET person_id = merged_from WHERE merged_from IS NOT NULL;
DROP INDEX bookings_merged_from_idx;
DROP INDEX bookings_one_active_key;
CREATE UNIQUE INDEX bookings_one_active_key ON bookings (lesson_id, person_id)
    WHERE status <> 'canceled' AND person_id IS NOT NULL;
ALTER TABLE bookings DROP CONSTRAINT bookings_merged_from_other, DROP COLUMN merged_from;

DROP TABLE person_duplicate_dismissals;
DROP TABLE import_runs;

-- Tombstones become persons again; whatever moved to the survivor stays.
DROP INDEX persons_merged_into_idx;
DROP INDEX persons_member_no_key;
ALTER TABLE persons DROP CONSTRAINT persons_tombstone_no_member_no,
    DROP CONSTRAINT persons_not_merged_into_self,
    DROP COLUMN merged_into,
    DROP COLUMN member_no;
