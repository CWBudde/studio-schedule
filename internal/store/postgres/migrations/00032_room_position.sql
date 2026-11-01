-- A room's position orders the rooms of its location (ADR 0007, amendment):
-- 1-based, mirroring domain.Room.Validate. Existing rooms keep today's order,
-- by name. There is no unique constraint: a reorder rewrites all positions of
-- a location in one transaction, and a tie (two concurrent creates) falls
-- back to the name.

-- +goose Up

ALTER TABLE rooms ADD COLUMN position integer;

UPDATE rooms r SET position = o.n
FROM (SELECT id, row_number() OVER (PARTITION BY location_id ORDER BY name, id) AS n FROM rooms) o
WHERE o.id = r.id;

ALTER TABLE rooms
    ALTER COLUMN position SET NOT NULL,
    ADD CONSTRAINT rooms_position_positive CHECK (position > 0);

-- +goose Down

ALTER TABLE rooms DROP COLUMN position;
