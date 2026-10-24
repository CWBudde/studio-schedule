-- name: GetLesson :one
SELECT * FROM lessons WHERE id = $1;

-- name: LockLesson :one
SELECT * FROM lessons WHERE id = $1 FOR UPDATE;

-- name: CreateLesson :exec
INSERT INTO lessons (
    id, class_id, course_type_id, room_id, instructor_id, accompanist_id, starts_at, ends_at, status,
    visibility, kind, capacity, slot_date
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
);

-- name: UpdateLesson :execrows
UPDATE lessons
SET class_id = $2, course_type_id = $3, room_id = $4, instructor_id = $5, accompanist_id = $6,
    starts_at = $7, ends_at = $8, status = $9, visibility = $10, kind = $11, capacity = $12,
    slot_date = $13
WHERE id = $1;

-- name: DeleteLesson :execrows
DELETE FROM lessons WHERE id = $1;

-- name: ListLessonsInRange :many
SELECT * FROM lessons
WHERE starts_at >= sqlc.arg(from_time) AND starts_at < sqlc.arg(to_time)
ORDER BY starts_at, id;

-- name: ListLessonsByClass :many
SELECT * FROM lessons
WHERE class_id = sqlc.arg(class_id) AND slot_date >= sqlc.arg(from_slot)
ORDER BY starts_at, id;
