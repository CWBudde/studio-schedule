-- +goose Up
-- Classes state who they are for (ADR 0021): a span between two of the
-- school's levels or all levels, a stage and an age range in months. The
-- old fixed level moves onto level rows: one per value a class uses, in the
-- old scale's order after the school's own levels; a level the school
-- already has by that name (ignoring case) is used as it is.
CREATE TEMPORARY TABLE old_levels (level text PRIMARY KEY, name text, code text, rank int) ON COMMIT DROP;
INSERT INTO old_levels VALUES
    ('beginner', 'Beginner', 'B', 1), ('intermediate', 'Intermediate', 'I', 2),
    ('advanced', 'Advanced', 'A', 3), ('professional', 'Professional', 'P', 4);

INSERT INTO levels (id, name, code, position)
SELECT gen_random_uuid(), o.name,
       CASE WHEN EXISTS (SELECT 1 FROM levels l WHERE lower(l.code) = lower(o.code)) THEN NULL ELSE o.code END,
       (SELECT coalesce(max(position), 0) FROM levels) + row_number() OVER (ORDER BY o.rank)
FROM old_levels o
WHERE o.level IN (SELECT level FROM classes)
  AND NOT EXISTS (SELECT 1 FROM levels l WHERE lower(l.name) = lower(o.name));

ALTER TABLE classes
    ADD COLUMN level_from_id uuid REFERENCES levels ON DELETE RESTRICT,
    ADD COLUMN level_to_id uuid REFERENCES levels ON DELETE RESTRICT,
    ADD COLUMN all_levels boolean NOT NULL DEFAULT false,
    ADD COLUMN stage smallint CHECK (stage BETWEEN 1 AND 9),
    ADD COLUMN age_from_months smallint CHECK (age_from_months BETWEEN 1 AND 1200),
    ADD COLUMN age_to_months smallint CHECK (age_to_months BETWEEN 1 AND 1200);

UPDATE classes c SET level_from_id = l.id, level_to_id = l.id
FROM old_levels o JOIN levels l ON lower(l.name) = lower(o.name)
WHERE c.level = o.level;
UPDATE classes SET all_levels = true WHERE level = 'open';

ALTER TABLE classes
    DROP COLUMN level,
    ADD CONSTRAINT classes_level_range CHECK ((level_from_id IS NULL) = (level_to_id IS NULL)),
    ADD CONSTRAINT classes_all_levels CHECK (NOT (all_levels AND level_from_id IS NOT NULL)),
    ADD CONSTRAINT classes_age_range CHECK (age_to_months IS NULL OR age_from_months IS NULL
                                            OR age_to_months >= age_from_months);
CREATE INDEX classes_level_from_idx ON classes (level_from_id);
CREATE INDEX classes_level_to_idx ON classes (level_to_id);

-- +goose Down
-- Lossy: a span keeps its lower end's old value if its name is one, else open.
-- The span's lower end is the one with the lower position (then ID), as the
-- service reads it; all levels and no level both become open. The levels
-- stay (00034's Down drops the table).
ALTER TABLE classes ADD COLUMN level text;

UPDATE classes c SET level = coalesce((
    SELECT lower(l.name) FROM levels l
    WHERE l.id IN (c.level_from_id, c.level_to_id)
    ORDER BY l.position, l.id
    LIMIT 1
), 'open');
UPDATE classes SET level = 'open'
WHERE level NOT IN ('beginner', 'intermediate', 'advanced', 'professional');

DROP INDEX classes_level_from_idx;
DROP INDEX classes_level_to_idx;

ALTER TABLE classes
    DROP CONSTRAINT classes_level_range,
    DROP CONSTRAINT classes_all_levels,
    DROP CONSTRAINT classes_age_range,
    DROP COLUMN level_from_id,
    DROP COLUMN level_to_id,
    DROP COLUMN all_levels,
    DROP COLUMN stage,
    DROP COLUMN age_from_months,
    DROP COLUMN age_to_months,
    ALTER COLUMN level SET NOT NULL,
    ADD CONSTRAINT classes_level_check
        CHECK (level IN ('beginner', 'intermediate', 'advanced', 'professional', 'open'));
