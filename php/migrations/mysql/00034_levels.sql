-- +goose Up

CREATE TABLE levels (
    id BINARY(16) PRIMARY KEY,
    name VARBINARY(3056) NOT NULL CHECK (OCTET_LENGTH(TRIM(name)) > 0),
    code VARBINARY(3056) CHECK (OCTET_LENGTH(TRIM(code)) > 0),
    position INT NOT NULL CHECK (position > 0),
    -- Match lower() under PostgreSQL's C collation: only fold ASCII A-Z.
    name_folded VARBINARY(3056) GENERATED ALWAYS AS (
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(CAST(name AS BINARY)
        , 'A', 'a'), 'B', 'b'), 'C', 'c'), 'D', 'd')
        , 'E', 'e'), 'F', 'f'), 'G', 'g'), 'H', 'h')
        , 'I', 'i'), 'J', 'j'), 'K', 'k'), 'L', 'l')
        , 'M', 'm'), 'N', 'n'), 'O', 'o'), 'P', 'p')
        , 'Q', 'q'), 'R', 'r'), 'S', 's'), 'T', 't')
        , 'U', 'u'), 'V', 'v'), 'W', 'w'), 'X', 'x')
        , 'Y', 'y'), 'Z', 'z')
    ) VIRTUAL,
    code_folded VARBINARY(3056) GENERATED ALWAYS AS (
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(CAST(code AS BINARY)
        , 'A', 'a'), 'B', 'b'), 'C', 'c'), 'D', 'd')
        , 'E', 'e'), 'F', 'f'), 'G', 'g'), 'H', 'h')
        , 'I', 'i'), 'J', 'j'), 'K', 'k'), 'L', 'l')
        , 'M', 'm'), 'N', 'n'), 'O', 'o'), 'P', 'p')
        , 'Q', 'q'), 'R', 'r'), 'S', 's'), 'T', 't')
        , 'U', 'u'), 'V', 'v'), 'W', 'w'), 'X', 'x')
        , 'Y', 'y'), 'Z', 'z')
    ) VIRTUAL,
    CONSTRAINT levels_name_key UNIQUE (name_folded),
    CONSTRAINT levels_code_key UNIQUE (code_folded)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down

DROP TABLE levels;
