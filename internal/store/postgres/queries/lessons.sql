-- name: GetLesson :one
SELECT * FROM lessons WHERE id = $1;

-- name: LockLesson :one
SELECT * FROM lessons WHERE id = $1 FOR UPDATE;

-- name: CreateLesson :exec
INSERT INTO lessons (
    id, class_id, course_type_id, room_id, instructor_id, accompanist_id, starts_at, ends_at, status,
    visibility, kind, capacity, slot_date, detached, cancel_note, canceled_at, canceled_by, renter, event_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
);

-- name: UpdateLesson :execrows
UPDATE lessons
SET class_id = $2, course_type_id = $3, room_id = $4, instructor_id = $5, accompanist_id = $6,
    starts_at = $7, ends_at = $8, status = $9, visibility = $10, kind = $11, capacity = $12,
    slot_date = $13, detached = $14, cancel_note = $15, canceled_at = $16, canceled_by = $17, renter = $18,
    event_id = $19
WHERE id = $1;

-- name: DeleteLesson :execrows
DELETE FROM lessons WHERE id = $1;

-- name: ListLessonsInRange :many
SELECT * FROM lessons
WHERE starts_at >= sqlc.arg(from_time) AND starts_at < sqlc.arg(to_time)
ORDER BY starts_at, id;

-- name: ListLessonsByIDs :many
SELECT * FROM lessons WHERE id = ANY(sqlc.arg(ids)::uuid[]) ORDER BY starts_at, id;

-- name: ListLessonsByEvent :many
SELECT * FROM lessons WHERE event_id = $1 ORDER BY starts_at, id;

-- name: ListLessonsByEvents :many
SELECT * FROM lessons WHERE event_id = ANY(sqlc.arg(events)::uuid[]) ORDER BY starts_at, id;

-- name: ListLessonsByStaff :many
SELECT * FROM lessons
WHERE (instructor_id = ANY(sqlc.arg(staff)::uuid[]) OR accompanist_id = ANY(sqlc.arg(staff)::uuid[]))
  AND starts_at >= sqlc.arg(from_time) AND starts_at < sqlc.arg(to_time)
ORDER BY starts_at, id;

-- name: ListLessonsByClassStartingAfter :many
SELECT * FROM lessons WHERE class_id = $1 AND starts_at > $2 ORDER BY starts_at, id;

-- name: ListLessonsByCourseTypeStartingAfter :many
SELECT * FROM lessons WHERE course_type_id = $1 AND starts_at > $2 ORDER BY starts_at, id;

-- name: ListLessonsByClass :many
SELECT * FROM lessons
WHERE class_id = sqlc.arg(class_id)
  AND (sqlc.narg(from_slot)::date IS NULL OR slot_date >= sqlc.narg(from_slot)::date)
ORDER BY starts_at, id;

-- name: ListRoomOverlaps :many
SELECT * FROM lessons
WHERE room_id = sqlc.arg(room_id)
  AND status <> 'canceled'
  AND starts_at < sqlc.arg(to_time) AND ends_at > sqlc.arg(from_time)
  AND (sqlc.narg(exclude_id)::uuid IS NULL OR id <> sqlc.narg(exclude_id)::uuid)
ORDER BY starts_at, id;
