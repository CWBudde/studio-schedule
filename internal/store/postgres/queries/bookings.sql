-- name: GetBooking :one
SELECT * FROM bookings WHERE id = $1;

-- name: CreateBooking :exec
INSERT INTO bookings (
    id, lesson_id, person_id, guest_name, guest_email, status, source, trial, prepaid, booked_at, canceled_at,
    late
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
);

-- name: UpdateBooking :execrows
UPDATE bookings
SET lesson_id = $2, person_id = $3, guest_name = $4, guest_email = $5, status = $6, source = $7, trial = $8,
    prepaid = $9, booked_at = $10, canceled_at = $11, late = $12
WHERE id = $1;

-- name: ListBookingsByLesson :many
SELECT * FROM bookings WHERE lesson_id = $1 ORDER BY booked_at, id;

-- name: CountActiveBookings :one
SELECT count(*) FROM bookings WHERE lesson_id = $1 AND status <> 'canceled';

-- name: CountActiveBookingsByLessons :many
SELECT lesson_id, count(*) AS active
FROM bookings
WHERE lesson_id = ANY(sqlc.arg(lessons)::uuid[]) AND status <> 'canceled'
GROUP BY lesson_id;

-- name: ListActiveBookingsByLessonsAndPersons :many
SELECT * FROM bookings
WHERE lesson_id = ANY(sqlc.arg(lessons)::uuid[]) AND person_id = ANY(sqlc.arg(persons)::uuid[])
    AND status <> 'canceled'
ORDER BY booked_at, id;

-- name: AddBookingEvent :exec
INSERT INTO booking_events (id, booking_id, at, status, actor_account_id, note, late, override)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListBookingEvents :many
SELECT * FROM booking_events WHERE booking_id = $1 ORDER BY at, id;
