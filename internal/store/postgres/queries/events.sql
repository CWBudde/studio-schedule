-- name: GetEvent :one
SELECT * FROM events WHERE id = $1;

-- name: GetEventBySlug :one
SELECT * FROM events WHERE slug = $1;

-- name: LockEvent :one
SELECT * FROM events WHERE id = $1 FOR UPDATE;

-- name: CreateEvent :exec
INSERT INTO events (
    id, slug, title, description, course_type_id, capacity, price, vat_rate_bp, status, signup_opens_at,
    signup_closes_at, cancel_until, created_at, updated_at, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
);

-- name: UpdateEvent :execrows
UPDATE events
SET slug = $2, title = $3, description = $4, course_type_id = $5, capacity = $6, price = $7, vat_rate_bp = $8,
    status = $9, signup_opens_at = $10, signup_closes_at = $11, cancel_until = $12, created_at = $13,
    updated_at = $14, created_by = $15
WHERE id = $1;

-- name: DeleteEvent :execrows
-- The image goes with the event (ON DELETE CASCADE).
DELETE FROM events WHERE id = $1;

-- name: ListEvents :many
-- By the start of the first session, canceled ones included; events without
-- a session last.
SELECT e.* FROM events e
ORDER BY (SELECT min(l.starts_at) FROM lessons l WHERE l.event_id = e.id) ASC NULLS LAST, e.id;

-- name: ListEventsByIDs :many
SELECT * FROM events WHERE id = ANY(sqlc.arg(ids)::uuid[]) ORDER BY id;

-- name: GetEventImage :one
-- A row for every existing event, NULLs if it has no image.
SELECT i.image, i.image_type
FROM events e LEFT JOIN event_images i ON i.event_id = e.id
WHERE e.id = $1;

-- name: GetEventImageInfo :one
-- The image's type and digest, without reading the image.
SELECT i.image_type, i.image_digest
FROM events e LEFT JOIN event_images i ON i.event_id = e.id
WHERE e.id = $1;

-- name: PutEventImage :execrows
-- No row if the event does not exist.
INSERT INTO event_images (event_id, image, image_type, image_digest)
SELECT e.id, sqlc.arg(image)::bytea, sqlc.arg(image_type)::text, sqlc.arg(image_digest)::text
FROM events e WHERE e.id = sqlc.arg(event_id)
ON CONFLICT (event_id) DO UPDATE SET
    image = excluded.image,
    image_type = excluded.image_type,
    image_digest = excluded.image_digest;

-- name: DeleteEventImage :one
-- Whether the event exists.
WITH deleted AS (
    DELETE FROM event_images WHERE event_id = sqlc.arg(event_id)
)
SELECT EXISTS (SELECT 1 FROM events WHERE id = sqlc.arg(event_id))::boolean AS found;

-- name: GetEventRegistration :one
SELECT * FROM event_registrations WHERE id = $1;

-- name: GetEventRegistrationByToken :one
SELECT * FROM event_registrations WHERE invite_token_hash = $1;

-- name: GetEventRegistrationByReceipt :one
SELECT * FROM event_registrations WHERE receipt_id = $1;

-- name: CreateEventRegistration :exec
INSERT INTO event_registrations (
    id, event_id, person_id, guest_name, guest_email, status, payment, price, vat_rate_bp, receipt_id,
    cancellation_receipt_id, invite_token_hash, invited_at, expires_at, registered_by, created_at, updated_at,
    canceled_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
);

-- name: UpdateEventRegistration :execrows
UPDATE event_registrations
SET event_id = $2, person_id = $3, guest_name = $4, guest_email = $5, status = $6, payment = $7, price = $8,
    vat_rate_bp = $9, receipt_id = $10, cancellation_receipt_id = $11, invite_token_hash = $12, invited_at = $13,
    expires_at = $14, registered_by = $15, created_at = $16, updated_at = $17, canceled_at = $18
WHERE id = $1;

-- name: ListEventRegistrationsByEvent :many
SELECT * FROM event_registrations WHERE event_id = $1 ORDER BY created_at, id;

-- name: ListEventRegistrationsByPersons :many
SELECT * FROM event_registrations
WHERE person_id = ANY(sqlc.arg(persons)::uuid[])
ORDER BY created_at, id;

-- name: ListEventRegistrationsByReceipts :many
SELECT * FROM event_registrations
WHERE receipt_id = ANY(sqlc.arg(receipts)::uuid[])
ORDER BY created_at, id;

-- name: CountEventRegistrationsByEvents :many
-- The open registrations at the instant: an invite holds its place until it
-- expires. Events whose counts are all zero are left out.
SELECT event_id,
    count(*) FILTER (WHERE status = 'registered')::integer AS registered,
    count(*) FILTER (WHERE status = 'invited' AND expires_at > sqlc.arg(at)::timestamptz)::integer AS invited,
    count(*) FILTER (WHERE status = 'waiting')::integer AS waiting
FROM event_registrations
WHERE event_id = ANY(sqlc.arg(events)::uuid[]) AND status IN ('waiting', 'invited', 'registered')
GROUP BY event_id
HAVING count(*) FILTER (
    WHERE status IN ('registered', 'waiting') OR expires_at > sqlc.arg(at)::timestamptz
) > 0;

-- name: AnonymizeGuestEventRegistrations :execrows
-- The guest registrations of the events whose last session (canceled ones
-- included) ended before the instant, unless anonymized already (ADR 0016,
-- ADR 0019). An event without sessions keeps its guests.
UPDATE event_registrations r
SET guest_name = sqlc.arg(guest_name), guest_email = NULL
WHERE r.person_id IS NULL
    AND (r.guest_name <> sqlc.arg(guest_name) OR r.guest_email IS NOT NULL)
    AND r.event_id IN (
        SELECT l.event_id FROM lessons l
        WHERE l.event_id IS NOT NULL
        GROUP BY l.event_id
        HAVING max(l.ends_at) < sqlc.arg(ended_before)::timestamptz
    );
