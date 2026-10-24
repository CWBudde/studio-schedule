-- name: GetTerm :one
SELECT * FROM terms WHERE id = $1;

-- name: LockTerm :one
SELECT * FROM terms WHERE id = $1 FOR UPDATE;

-- name: CreateTerm :exec
INSERT INTO terms (id, name, starts_on, ends_on) VALUES ($1, $2, $3, $4);

-- name: UpdateTerm :execrows
UPDATE terms SET name = $2, starts_on = $3, ends_on = $4 WHERE id = $1;

-- name: DeleteTerm :execrows
DELETE FROM terms WHERE id = $1;

-- name: ListTerms :many
SELECT * FROM terms ORDER BY starts_on, id;

-- name: GetClass :one
SELECT * FROM classes WHERE id = $1;

-- name: LockClass :one
SELECT * FROM classes WHERE id = $1 FOR UPDATE;

-- name: CreateClass :exec
INSERT INTO classes (
    id, term_id, course_type_id, room_id, instructor_id, accompanist_id, level, weekday, start_time,
    duration, capacity, retired
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
);

-- name: UpdateClass :execrows
UPDATE classes
SET term_id = $2, course_type_id = $3, room_id = $4, instructor_id = $5, accompanist_id = $6, level = $7,
    weekday = $8, start_time = $9, duration = $10, capacity = $11, retired = $12
WHERE id = $1;

-- name: DeleteClass :execrows
DELETE FROM classes WHERE id = $1;

-- name: ListClassesByTerm :many
SELECT * FROM classes WHERE term_id = $1 ORDER BY weekday, start_time, id;

-- name: AddClassMember :exec
INSERT INTO class_members (class_id, person_id, since) VALUES ($1, $2, $3);

-- name: RemoveClassMember :execrows
DELETE FROM class_members WHERE class_id = $1 AND person_id = $2;

-- name: ListClassMembers :many
SELECT * FROM class_members WHERE class_id = $1 ORDER BY person_id;

-- name: ListPersonClassMemberships :many
SELECT * FROM class_members WHERE person_id = $1 ORDER BY class_id;
