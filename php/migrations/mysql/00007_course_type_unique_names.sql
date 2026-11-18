-- Course type names are unique, with the ASCII-only fold and duplicate
-- handling from 00006.

-- +goose Up

ALTER TABLE course_types ADD COLUMN name_folded VARBINARY(3056) GENERATED ALWAYS AS (
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
        REPLACE(REPLACE(CAST(name AS BINARY), 'A', 'a'), 'B', 'b'), 'C', 'c'), 'D', 'd'), 'E', 'e'), 'F', 'f'),
        'G', 'g'), 'H', 'h'), 'I', 'i'), 'J', 'j'), 'K', 'k'), 'L', 'l'),
        'M', 'm'), 'N', 'n'), 'O', 'o'), 'P', 'p'), 'Q', 'q'), 'R', 'r'),
        'S', 's'), 'T', 't'), 'U', 'u'), 'V', 'v'), 'W', 'w'), 'X', 'x'),
        'Y', 'y'), 'Z', 'z')
) VIRTUAL;

UPDATE course_types c
JOIN course_types o ON o.name_folded = c.name_folded AND o.id < c.id
SET c.name = CONCAT(c.name, ' (',
    LOWER(CONCAT(SUBSTRING(HEX(c.id), 1, 8), '-', SUBSTRING(HEX(c.id), 9, 4), '-',
        SUBSTRING(HEX(c.id), 13, 4), '-', SUBSTRING(HEX(c.id), 17, 4), '-',
        SUBSTRING(HEX(c.id), 21, 12))), ')');

CREATE UNIQUE INDEX course_types_name_key ON course_types (name_folded);

-- +goose Down

ALTER TABLE course_types DROP INDEX course_types_name_key, DROP COLUMN name_folded;
