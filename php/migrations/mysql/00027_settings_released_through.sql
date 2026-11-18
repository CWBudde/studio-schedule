-- +goose Up

ALTER TABLE settings ADD COLUMN released_through DATE;

-- +goose Down

ALTER TABLE settings DROP COLUMN released_through;
