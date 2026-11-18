-- +goose Up

-- Reuse existing named levels and append the legacy levels currently in use
-- in their old order. Only keep the default codes that are still available.
CREATE TEMPORARY TABLE old_levels (
    level VARBINARY(32) PRIMARY KEY,
    name VARCHAR(32) CHARACTER SET ascii NOT NULL,
    code VARCHAR(1) CHARACTER SET ascii NOT NULL,
    sort_position INT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

INSERT INTO old_levels (level, name, code, sort_position) VALUES
    ('beginner', 'Beginner', 'B', 1), ('intermediate', 'Intermediate', 'I', 2),
    ('advanced', 'Advanced', 'A', 3), ('professional', 'Professional', 'P', 4);

INSERT INTO levels (id, name, code, position)
SELECT UNHEX(REPLACE(UUID(), '-', '')), o.name,
       CASE WHEN EXISTS (SELECT 1 FROM levels l WHERE l.code_folded = LOWER(o.code)) THEN NULL ELSE o.code END,
       (SELECT COALESCE(MAX(position), 0) FROM levels) + ROW_NUMBER() OVER (ORDER BY o.sort_position)
FROM old_levels o
WHERE o.level IN (SELECT level FROM classes)
  AND NOT EXISTS (SELECT 1 FROM levels l WHERE l.name_folded = LOWER(o.name));

ALTER TABLE classes
    ADD COLUMN level_from_id BINARY(16),
    ADD COLUMN level_to_id BINARY(16),
    ADD COLUMN all_levels BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN stage SMALLINT,
    ADD COLUMN age_from_months SMALLINT,
    ADD COLUMN age_to_months SMALLINT,
    ADD INDEX classes_level_from_idx (level_from_id),
    ADD INDEX classes_level_to_idx (level_to_id),
    ADD CONSTRAINT classes_level_from_id_fkey FOREIGN KEY (level_from_id) REFERENCES levels (id) ON DELETE RESTRICT,
    ADD CONSTRAINT classes_level_to_id_fkey FOREIGN KEY (level_to_id) REFERENCES levels (id) ON DELETE RESTRICT,
    ADD CONSTRAINT classes_stage_check CHECK (stage BETWEEN 1 AND 9),
    ADD CONSTRAINT classes_age_from_months_check CHECK (age_from_months BETWEEN 1 AND 1200),
    ADD CONSTRAINT classes_age_to_months_check CHECK (age_to_months BETWEEN 1 AND 1200);

UPDATE classes c
JOIN old_levels o ON o.level = c.level
JOIN levels l ON l.name_folded = LOWER(o.name)
SET c.level_from_id = l.id, c.level_to_id = l.id;

UPDATE classes SET all_levels = true WHERE level = 'open';

DROP TEMPORARY TABLE old_levels;

ALTER TABLE classes
    DROP CONSTRAINT classes_level_check,
    DROP COLUMN level,
    ADD CONSTRAINT classes_level_range CHECK ((level_from_id IS NULL) = (level_to_id IS NULL)),
    ADD CONSTRAINT classes_all_levels CHECK (NOT (all_levels AND level_from_id IS NOT NULL)),
    ADD CONSTRAINT classes_age_range CHECK (
        age_to_months IS NULL OR age_from_months IS NULL OR age_to_months >= age_from_months
    );

-- +goose Down

-- A range keeps its lower level if it has one of the legacy names. Unknown
-- names, all levels and no level become open. The level catalog stays.
ALTER TABLE classes ADD COLUMN level VARBINARY(32);

UPDATE classes c
SET level = CASE (
    SELECT l.name_folded FROM levels l
    WHERE l.id IN (c.level_from_id, c.level_to_id)
    ORDER BY l.position, l.id
    LIMIT 1
)
    WHEN 'beginner' THEN 'beginner'
    WHEN 'intermediate' THEN 'intermediate'
    WHEN 'advanced' THEN 'advanced'
    WHEN 'professional' THEN 'professional'
    ELSE 'open'
END;

ALTER TABLE classes
    DROP FOREIGN KEY classes_level_from_id_fkey,
    DROP FOREIGN KEY classes_level_to_id_fkey,
    DROP CONSTRAINT classes_level_range,
    DROP CONSTRAINT classes_all_levels,
    DROP CONSTRAINT classes_age_range,
    DROP CONSTRAINT classes_stage_check,
    DROP CONSTRAINT classes_age_from_months_check,
    DROP CONSTRAINT classes_age_to_months_check;

ALTER TABLE classes
    DROP INDEX classes_level_from_idx,
    DROP INDEX classes_level_to_idx,
    DROP COLUMN level_from_id,
    DROP COLUMN level_to_id,
    DROP COLUMN all_levels,
    DROP COLUMN stage,
    DROP COLUMN age_from_months,
    DROP COLUMN age_to_months,
    MODIFY COLUMN level VARBINARY(32) NOT NULL,
    ADD CONSTRAINT classes_level_check
        CHECK (level IN ('beginner', 'intermediate', 'advanced', 'professional', 'open'));
