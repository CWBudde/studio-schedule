-- name: GetAnnouncement :one
SELECT * FROM announcements WHERE id = $1;

-- name: LockAnnouncement :one
SELECT * FROM announcements WHERE id = $1 FOR UPDATE;

-- name: CreateAnnouncement :exec
INSERT INTO announcements (
    id, author_id, title, body, audience, role, class_id, course_type_id, lesson_id, publish_at, expires_at,
    email, emailed_at, email_recipients, mail_cursor, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17);

-- name: UpdateAnnouncement :execrows
-- Everything but the mail state, which only SetAnnouncementMailState writes.
UPDATE announcements
SET author_id = $2, title = $3, body = $4, audience = $5, role = $6, class_id = $7, course_type_id = $8,
    lesson_id = $9, publish_at = $10, expires_at = $11, email = $12, created_at = $13, updated_at = $14
WHERE id = $1;

-- name: SetAnnouncementMailState :execrows
UPDATE announcements SET emailed_at = $2, email_recipients = $3, mail_cursor = $4 WHERE id = $1;

-- name: DeleteAnnouncement :execrows
DELETE FROM announcements WHERE id = $1;

-- name: ListAnnouncements :many
SELECT * FROM announcements ORDER BY publish_at DESC, id DESC;

-- name: ListShownAnnouncements :many
SELECT * FROM announcements
WHERE publish_at <= sqlc.arg(now) AND (expires_at IS NULL OR expires_at > sqlc.arg(now))
ORDER BY publish_at DESC, id DESC;

-- name: ListAnnouncementsDueForMail :many
-- SKIP LOCKED lets concurrent jobs each take different announcements.
SELECT * FROM announcements
WHERE email AND emailed_at IS NULL AND publish_at <= sqlc.arg(now)
ORDER BY publish_at, id
LIMIT sqlc.arg(max_rows)
FOR UPDATE SKIP LOCKED;

-- name: CountAnnouncementsScheduledForMail :one
SELECT count(*) FROM announcements
WHERE email AND emailed_at IS NULL AND publish_at >= sqlc.arg(from_at) AND publish_at < sqlc.arg(to_at)
    AND id IS DISTINCT FROM sqlc.narg(except_id)::uuid;

-- name: LockAnnouncementMail :one
-- A transaction-scoped advisory lock serializes everything that checks or
-- uses the daily limit of emailed announcements. The key is arbitrary but
-- fixed: hashtext('announcement-mail'). A read-only transaction, which an
-- advisory lock would not refuse, gets false and no lock.
SELECT CASE WHEN current_setting('transaction_read_only')::boolean THEN false
    ELSE (SELECT true FROM (SELECT pg_advisory_xact_lock(hashtext('announcement-mail'))) AS l)
END::boolean AS locked;

-- name: DismissAnnouncement :exec
INSERT INTO announcement_dismissals (announcement_id, account_id, dismissed_at) VALUES ($1, $2, $3)
ON CONFLICT (account_id, announcement_id) DO NOTHING;

-- name: ListDismissedAnnouncements :many
SELECT announcement_id FROM announcement_dismissals
WHERE account_id = sqlc.arg(account_id) AND announcement_id = ANY(sqlc.arg(announcements)::uuid[])
ORDER BY announcement_id;

-- name: CreateAnnouncementMailing :exec
INSERT INTO announcement_mailings (id, announcement_id, sender_id, sent_at, recipients) VALUES ($1, $2, $3, $4, $5);

-- name: ListAnnouncementMailings :many
SELECT * FROM announcement_mailings
WHERE sent_at >= sqlc.arg(from_at) AND sent_at < sqlc.arg(to_at)
ORDER BY sent_at, id;
