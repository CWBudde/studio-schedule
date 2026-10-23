-- Course type names are unique, ignoring case (ADR 0007), like location
-- names in 00006 and with the same treatment of existing duplicates.

-- +goose Up

UPDATE course_types c SET name = c.name || ' (' || c.id || ')'
WHERE EXISTS (SELECT FROM course_types o WHERE lower(o.name) = lower(c.name) AND o.id < c.id);

CREATE UNIQUE INDEX course_types_name_key ON course_types (lower(name));

-- +goose Down

DROP INDEX course_types_name_key;
