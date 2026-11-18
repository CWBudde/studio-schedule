-- +goose Up

ALTER TABLE rooms ADD COLUMN position INT;

-- Sort by UTF-8 bytes to reproduce PostgreSQL's C collation, including spaces.
UPDATE rooms r
JOIN (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY location_id ORDER BY CAST(name AS BINARY), id) AS room_position
    FROM rooms
) ordered_rooms ON ordered_rooms.id = r.id
SET r.position = ordered_rooms.room_position;

ALTER TABLE rooms
    MODIFY COLUMN position INT NOT NULL,
    ADD CONSTRAINT rooms_position_positive CHECK (position > 0);

-- +goose Down

ALTER TABLE rooms DROP CONSTRAINT rooms_position_positive, DROP COLUMN position;
