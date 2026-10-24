-- name: GetLocation :one
SELECT * FROM locations WHERE id = $1;

-- name: CreateLocation :exec
INSERT INTO locations (id, name, address) VALUES ($1, $2, $3);

-- name: UpdateLocation :execrows
UPDATE locations SET name = $2, address = $3 WHERE id = $1;

-- name: DeleteLocation :execrows
DELETE FROM locations WHERE id = $1;

-- name: ListLocations :many
SELECT * FROM locations ORDER BY name, id;

-- name: GetRoom :one
SELECT * FROM rooms WHERE id = $1;

-- name: CreateRoom :exec
INSERT INTO rooms (id, location_id, name, capacity) VALUES ($1, $2, $3, $4);

-- name: UpdateRoom :execrows
UPDATE rooms SET location_id = $2, name = $3, capacity = $4 WHERE id = $1;

-- name: DeleteRoom :execrows
DELETE FROM rooms WHERE id = $1;

-- name: ListRooms :many
SELECT * FROM rooms ORDER BY name, id;

-- name: ListRoomsByLocation :many
SELECT * FROM rooms WHERE location_id = $1 ORDER BY name, id;

-- name: GetStaff :one
SELECT * FROM staff WHERE id = $1;

-- name: CreateStaff :exec
INSERT INTO staff (id, person_id, display_name, kind, active) VALUES ($1, $2, $3, $4, $5);

-- name: UpdateStaff :execrows
UPDATE staff SET person_id = $2, display_name = $3, kind = $4, active = $5 WHERE id = $1;

-- name: DeleteStaff :execrows
DELETE FROM staff WHERE id = $1;

-- name: ListStaff :many
SELECT * FROM staff ORDER BY display_name, id;

-- name: ListStaffByPerson :many
SELECT * FROM staff WHERE person_id = $1 ORDER BY display_name, id;

-- name: GetCourseType :one
SELECT * FROM course_types WHERE id = $1;

-- name: CreateCourseType :exec
INSERT INTO course_types (id, name, description) VALUES ($1, $2, $3);

-- name: UpdateCourseType :execrows
UPDATE course_types SET name = $2, description = $3 WHERE id = $1;

-- name: DeleteCourseType :execrows
DELETE FROM course_types WHERE id = $1;

-- name: ListCourseTypes :many
SELECT * FROM course_types ORDER BY name, id;
