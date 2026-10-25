-- name: GetBooking :one
SELECT * FROM bookings WHERE id = $1;

-- name: CreateBooking :exec
INSERT INTO bookings (
    id, lesson_id, person_id, guest_name, guest_email, status, source, trial, booked_at, canceled_at,
    late, released, trial_note, merged_from
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
);

-- name: UpdateBooking :execrows
UPDATE bookings
SET lesson_id = $2, person_id = $3, guest_name = $4, guest_email = $5, status = $6, source = $7, trial = $8,
    booked_at = $9, canceled_at = $10, late = $11, released = $12, trial_note = $13, merged_from = $14
WHERE id = $1;

-- name: ListBookingsByIDs :many
SELECT * FROM bookings WHERE id = ANY(sqlc.arg(ids)::uuid[]) ORDER BY booked_at, id;

-- name: ListBookingsByLesson :many
SELECT * FROM bookings WHERE lesson_id = $1 ORDER BY booked_at, id;

-- name: CountActiveBookings :one
SELECT count(*) FROM bookings WHERE lesson_id = $1 AND status <> 'canceled';

-- name: CountActiveBookingsByLessons :many
SELECT lesson_id, count(*) AS active
FROM bookings
WHERE lesson_id = ANY(sqlc.arg(lessons)::uuid[]) AND status <> 'canceled'
GROUP BY lesson_id;

-- name: CountBookingsByLessons :many
SELECT lesson_id,
    count(*) AS active,
    count(*) FILTER (WHERE trial) AS trials,
    count(*) FILTER (WHERE person_id IS NULL) AS guests,
    count(*) FILTER (WHERE status = 'booked') AS booked,
    count(*) FILTER (WHERE status = 'attended') AS attended,
    count(*) FILTER (WHERE status = 'no_show') AS no_show
FROM bookings
WHERE lesson_id = ANY(sqlc.arg(lessons)::uuid[]) AND status <> 'canceled'
GROUP BY lesson_id;

-- name: CountLateCanceledBookingsByLessons :many
SELECT lesson_id, count(*) AS late
FROM bookings
WHERE lesson_id = ANY(sqlc.arg(lessons)::uuid[]) AND status = 'canceled' AND late
GROUP BY lesson_id;

-- name: CountActiveBookingPersons :one
SELECT count(DISTINCT person_id) FROM bookings
WHERE lesson_id = ANY(sqlc.arg(lessons)::uuid[]) AND status <> 'canceled' AND person_id IS NOT NULL;

-- name: ListActiveBookingsByLessons :many
SELECT * FROM bookings
WHERE lesson_id = ANY(sqlc.arg(lessons)::uuid[]) AND status <> 'canceled'
ORDER BY booked_at, id;

-- name: ListActiveBookingsByLessonsAndPersons :many
SELECT * FROM bookings
WHERE lesson_id = ANY(sqlc.arg(lessons)::uuid[]) AND person_id = ANY(sqlc.arg(persons)::uuid[])
    AND status <> 'canceled'
ORDER BY booked_at, id;

-- name: DeleteBookingEventsByLessonAndSource :exec
DELETE FROM booking_events
WHERE booking_id IN (SELECT id FROM bookings WHERE lesson_id = $1 AND source = $2);

-- name: DeleteBookingsByLessonAndSource :execrows
DELETE FROM bookings WHERE lesson_id = $1 AND source = $2;

-- name: AnonymizeGuestBookings :execrows
-- The guest bookings of the lessons that ended before the instant, unless
-- anonymized already (ADR 0016).
UPDATE bookings b
SET guest_name = sqlc.arg(guest_name), guest_email = NULL, trial_note = NULL
FROM lessons l
WHERE l.id = b.lesson_id AND b.person_id IS NULL AND l.ends_at < sqlc.arg(ended_before)
    AND (b.guest_name <> sqlc.arg(guest_name) OR b.guest_email IS NOT NULL OR b.trial_note IS NOT NULL);

-- name: ListBookingsByLessonsAndPersons :many
SELECT * FROM bookings
WHERE lesson_id = ANY(sqlc.arg(lessons)::uuid[]) AND person_id = ANY(sqlc.arg(persons)::uuid[])
ORDER BY booked_at, id;

-- ListBookingsByPersons and ListBookingsByPersonsDesc are one query in two
-- directions (sqlc cannot parametrize ORDER BY): keep their WHERE clauses
-- identical, and their orders each other's reverse.

-- name: ListBookingsByPersons :many
-- A NULL bound is left out, a NULL limit is none.
SELECT b.* FROM bookings b
JOIN lessons l ON l.id = b.lesson_id
WHERE b.person_id = ANY(sqlc.arg(persons)::uuid[])
    AND (NOT sqlc.arg(active_only)::boolean OR b.status <> 'canceled')
    AND (sqlc.narg(starts_from)::timestamptz IS NULL OR l.starts_at >= sqlc.narg(starts_from))
    AND (sqlc.narg(starts_before)::timestamptz IS NULL OR l.starts_at < sqlc.narg(starts_before))
    AND (sqlc.narg(ends_after)::timestamptz IS NULL OR l.ends_at > sqlc.narg(ends_after))
    AND (sqlc.narg(ends_by)::timestamptz IS NULL OR l.ends_at <= sqlc.narg(ends_by))
ORDER BY l.starts_at, l.id, b.booked_at, b.id
LIMIT sqlc.narg(max_rows)::integer;

-- name: ListBookingsByPersonsDesc :many
-- ListBookingsByPersons, newest first.
SELECT b.* FROM bookings b
JOIN lessons l ON l.id = b.lesson_id
WHERE b.person_id = ANY(sqlc.arg(persons)::uuid[])
    AND (NOT sqlc.arg(active_only)::boolean OR b.status <> 'canceled')
    AND (sqlc.narg(starts_from)::timestamptz IS NULL OR l.starts_at >= sqlc.narg(starts_from))
    AND (sqlc.narg(starts_before)::timestamptz IS NULL OR l.starts_at < sqlc.narg(starts_before))
    AND (sqlc.narg(ends_after)::timestamptz IS NULL OR l.ends_at > sqlc.narg(ends_after))
    AND (sqlc.narg(ends_by)::timestamptz IS NULL OR l.ends_at <= sqlc.narg(ends_by))
ORDER BY l.starts_at DESC, l.id DESC, b.booked_at DESC, b.id DESC
LIMIT sqlc.narg(max_rows)::integer;

-- name: AddBookingEvent :exec
INSERT INTO booking_events (id, booking_id, at, status, actor_account_id, note, late, override)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListBookingEvents :many
SELECT * FROM booking_events WHERE booking_id = $1 ORDER BY at, id;
