-- name: GetBooking :one
SELECT * FROM bookings WHERE id = $1;

-- name: CreateBooking :exec
INSERT INTO bookings (
    id, lesson_id, person_id, guest_name, guest_email, status, trial, prepaid, booked_at, canceled_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
);

-- name: UpdateBooking :execrows
UPDATE bookings
SET lesson_id = $2, person_id = $3, guest_name = $4, guest_email = $5, status = $6, trial = $7,
    prepaid = $8, booked_at = $9, canceled_at = $10
WHERE id = $1;

-- name: ListBookingsByLesson :many
SELECT * FROM bookings WHERE lesson_id = $1 ORDER BY booked_at, id;

-- name: CountActiveBookings :one
SELECT count(*) FROM bookings WHERE lesson_id = $1 AND status <> 'canceled';

-- name: AddBookingEvent :exec
INSERT INTO booking_events (id, booking_id, at, status, actor_account_id, note)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListBookingEvents :many
SELECT * FROM booking_events WHERE booking_id = $1 ORDER BY at, id;
