-- +goose Up
-- The school's level scale (ADR 0021): names are the school's own, position
-- orders the scale, code is an optional short label. Names and codes are
-- unique ignoring case, like course type names (and in collation "C" like
-- them, so lower() folds ASCII letters only, as the memory store does).
CREATE TABLE levels (
    id uuid PRIMARY KEY,
    name text COLLATE "C" NOT NULL CHECK (btrim(name) <> ''),
    code text COLLATE "C" CHECK (btrim(code) <> ''),
    position integer NOT NULL CHECK (position > 0)
);
CREATE UNIQUE INDEX levels_name_key ON levels (lower(name));
CREATE UNIQUE INDEX levels_code_key ON levels (lower(code));

-- +goose Down
DROP TABLE levels;
