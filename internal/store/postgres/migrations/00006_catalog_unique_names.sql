-- Location names are unique within the school and room names within their
-- location, ignoring case (ADR 0007). Conventions as in 00001_core.sql; under
-- collation "C", lower() folds ASCII letters only, as the memory store does.

-- +goose Up

-- Names that differ only in case were allowed before. The first of them (by
-- ID) keeps its name; the others get their ID appended, so the indexes can be
-- built and the office can rename them. The down migration keeps the names.
UPDATE locations l SET name = l.name || ' (' || l.id || ')'
WHERE EXISTS (SELECT FROM locations o WHERE lower(o.name) = lower(l.name) AND o.id < l.id);

UPDATE rooms r SET name = r.name || ' (' || r.id || ')'
WHERE EXISTS (
    SELECT FROM rooms o
    WHERE o.location_id = r.location_id AND lower(o.name) = lower(r.name) AND o.id < r.id
);

CREATE UNIQUE INDEX locations_name_key ON locations (lower(name));

CREATE UNIQUE INDEX rooms_location_name_key ON rooms (location_id, lower(name));

-- +goose Down

DROP INDEX rooms_location_name_key;

DROP INDEX locations_name_key;
