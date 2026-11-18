-- +goose Up

ALTER TABLE persons
    ADD COLUMN member_no VARBINARY(3072),
    ADD COLUMN merged_into BINARY(16),
    ADD CONSTRAINT persons_member_no_check CHECK (OCTET_LENGTH(member_no) > 0 AND member_no = TRIM(member_no)),
    ADD CONSTRAINT persons_not_merged_into_self CHECK (merged_into <> id),
    ADD CONSTRAINT persons_tombstone_no_member_no CHECK (merged_into IS NULL OR member_no IS NULL),
    ADD UNIQUE KEY persons_member_no_key (member_no),
    ADD KEY persons_merged_into_idx (merged_into),
    ADD CONSTRAINT persons_merged_into_fkey FOREIGN KEY (merged_into) REFERENCES persons(id) ON DELETE RESTRICT;

CREATE TABLE import_runs (
    id BINARY(16) PRIMARY KEY,
    file_name LONGTEXT NOT NULL,
    -- NULL for the command line.
    actor_id BINARY(16),
    started_at DATETIME(6) NOT NULL,
    finished_at DATETIME(6) NOT NULL,
    rows_total INT NOT NULL,
    created INT NOT NULL,
    updated INT NOT NULL,
    unchanged INT NOT NULL,
    failed INT NOT NULL,
    guardians_created INT NOT NULL,
    guardians_updated INT NOT NULL,
    links_added INT NOT NULL,
    errors JSON,
    CONSTRAINT import_runs_file_name_check CHECK (OCTET_LENGTH(TRIM(file_name)) > 0),
    CONSTRAINT import_runs_finished_at_check CHECK (finished_at >= started_at),
    CONSTRAINT import_runs_rows_total_check CHECK (rows_total >= 0),
    CONSTRAINT import_runs_created_check CHECK (created >= 0),
    CONSTRAINT import_runs_updated_check CHECK (updated >= 0),
    CONSTRAINT import_runs_unchanged_check CHECK (unchanged >= 0),
    CONSTRAINT import_runs_failed_check CHECK (failed >= 0),
    CONSTRAINT import_runs_guardians_created_check CHECK (guardians_created >= 0),
    CONSTRAINT import_runs_guardians_updated_check CHECK (guardians_updated >= 0),
    CONSTRAINT import_runs_links_added_check CHECK (links_added >= 0),
    CONSTRAINT import_runs_errors_check CHECK (JSON_TYPE(errors) = 'ARRAY' AND JSON_LENGTH(errors) > 0),
    CONSTRAINT import_runs_rows CHECK (rows_total = created + updated + unchanged + failed),
    KEY import_runs_started_idx (started_at DESC, id DESC),
    KEY import_runs_actor_id_fkey (actor_id),
    CONSTRAINT import_runs_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE person_duplicate_dismissals (
    person_a BINARY(16) NOT NULL,
    person_b BINARY(16) NOT NULL,
    dismissed_by BINARY(16) NOT NULL,
    dismissed_at DATETIME(6) NOT NULL,
    PRIMARY KEY (person_a, person_b),
    CONSTRAINT person_duplicate_dismissals_ordered CHECK (person_a < person_b),
    KEY person_duplicate_dismissals_person_b_idx (person_b),
    KEY person_duplicate_dismissals_dismissed_by_fkey (dismissed_by),
    CONSTRAINT person_duplicate_dismissals_person_a_fkey FOREIGN KEY (person_a) REFERENCES persons(id) ON DELETE CASCADE,
    CONSTRAINT person_duplicate_dismissals_person_b_fkey FOREIGN KEY (person_b) REFERENCES persons(id) ON DELETE CASCADE,
    CONSTRAINT person_duplicate_dismissals_dismissed_by_fkey
        FOREIGN KEY (dismissed_by) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- A booking kept as merge history does not participate in the active key.
ALTER TABLE bookings
    ADD COLUMN merged_from BINARY(16),
    ADD CONSTRAINT bookings_merged_from_other CHECK (merged_from <> person_id),
    ADD KEY bookings_merged_from_idx (merged_from),
    ADD CONSTRAINT bookings_merged_from_fkey FOREIGN KEY (merged_from) REFERENCES persons(id) ON DELETE RESTRICT;

ALTER TABLE bookings
    DROP INDEX bookings_one_active_key,
    MODIFY COLUMN active_person_id BINARY(16) GENERATED ALWAYS AS (
        CASE WHEN status <> 'canceled' AND merged_from IS NULL THEN person_id END
    ) VIRTUAL,
    ADD UNIQUE KEY bookings_one_active_key (lesson_id, active_person_id);

INSERT INTO audit_actions (action) VALUES ('person-merged');

-- +goose Down

DELETE FROM audit_events WHERE action = 'person-merged';
DELETE FROM audit_actions WHERE action = 'person-merged';

-- Restore each historical booking to the tombstone it came from before
-- putting it back in the active key.
ALTER TABLE bookings DROP CONSTRAINT bookings_merged_from_other;
UPDATE bookings SET person_id = merged_from WHERE merged_from IS NOT NULL;
ALTER TABLE bookings
    DROP INDEX bookings_one_active_key,
    MODIFY COLUMN active_person_id BINARY(16) GENERATED ALWAYS AS (
        CASE WHEN status <> 'canceled' THEN person_id END
    ) VIRTUAL,
    ADD UNIQUE KEY bookings_one_active_key (lesson_id, active_person_id);

ALTER TABLE bookings DROP FOREIGN KEY bookings_merged_from_fkey;
ALTER TABLE bookings
    DROP INDEX bookings_merged_from_idx,
    DROP COLUMN merged_from;

DROP TABLE person_duplicate_dismissals;
DROP TABLE import_runs;

-- Tombstones become persons again.
ALTER TABLE persons DROP FOREIGN KEY persons_merged_into_fkey;
ALTER TABLE persons
    DROP CONSTRAINT persons_tombstone_no_member_no,
    DROP CONSTRAINT persons_not_merged_into_self,
    DROP CONSTRAINT persons_member_no_check,
    DROP INDEX persons_merged_into_idx,
    DROP INDEX persons_member_no_key,
    DROP COLUMN merged_into,
    DROP COLUMN member_no;
