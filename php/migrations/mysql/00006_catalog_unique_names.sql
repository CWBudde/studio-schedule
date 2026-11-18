-- Location names are unique within the school and room names within their
-- location (ADR 0007). The generated keys fold ASCII letters only, as in 00001.

-- +goose Up

ALTER TABLE locations ADD COLUMN name_folded VARBINARY(3056) GENERATED ALWAYS AS (
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

ALTER TABLE rooms ADD COLUMN name_folded VARBINARY(3056) GENERATED ALWAYS AS (
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

-- The lowest ID keeps its name; other duplicates get their UUID appended.
-- The down migration keeps the resulting names.
UPDATE locations l
JOIN locations o ON o.name_folded = l.name_folded AND o.id < l.id
SET l.name = CONCAT(l.name, ' (',
    LOWER(CONCAT(SUBSTRING(HEX(l.id), 1, 8), '-', SUBSTRING(HEX(l.id), 9, 4), '-',
        SUBSTRING(HEX(l.id), 13, 4), '-', SUBSTRING(HEX(l.id), 17, 4), '-',
        SUBSTRING(HEX(l.id), 21, 12))), ')');

UPDATE rooms r
JOIN rooms o ON o.location_id = r.location_id AND o.name_folded = r.name_folded AND o.id < r.id
SET r.name = CONCAT(r.name, ' (',
    LOWER(CONCAT(SUBSTRING(HEX(r.id), 1, 8), '-', SUBSTRING(HEX(r.id), 9, 4), '-',
        SUBSTRING(HEX(r.id), 13, 4), '-', SUBSTRING(HEX(r.id), 17, 4), '-',
        SUBSTRING(HEX(r.id), 21, 12))), ')');

CREATE UNIQUE INDEX locations_name_key ON locations (name_folded);
CREATE UNIQUE INDEX rooms_location_name_key ON rooms (location_id, name_folded);

-- +goose Down

ALTER TABLE rooms DROP INDEX rooms_location_name_key, DROP COLUMN name_folded;

ALTER TABLE locations DROP INDEX locations_name_key, DROP COLUMN name_folded;
