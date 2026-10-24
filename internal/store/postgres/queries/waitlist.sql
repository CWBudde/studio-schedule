-- name: GetWaitlistEntry :one
SELECT * FROM waitlist_entries WHERE id = $1;

-- name: GetWaitlistEntryByToken :one
SELECT * FROM waitlist_entries WHERE invite_token_hash = $1;

-- name: CreateWaitlistEntry :exec
INSERT INTO waitlist_entries (
    id, lesson_id, person_id, status, joined_at, stay_on_list, joined_by, invite_token_hash, invited_at, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
);

-- name: UpdateWaitlistEntry :execrows
UPDATE waitlist_entries
SET lesson_id = $2, person_id = $3, status = $4, joined_at = $5, stay_on_list = $6, joined_by = $7,
    invite_token_hash = $8, invited_at = $9, expires_at = $10
WHERE id = $1;

-- name: ListWaitlistEntriesByLesson :many
SELECT * FROM waitlist_entries WHERE lesson_id = $1 ORDER BY joined_at, id;

-- name: ListOpenWaitlistEntriesByLessons :many
SELECT * FROM waitlist_entries
WHERE lesson_id = ANY(sqlc.arg(lessons)::uuid[]) AND status IN ('waiting', 'invited')
ORDER BY joined_at, id;

-- name: ListWaitlistEntriesByPersons :many
SELECT * FROM waitlist_entries
WHERE person_id = ANY(sqlc.arg(persons)::uuid[])
ORDER BY joined_at, id;

-- name: ListDueWaitlistLessons :many
SELECT DISTINCT w.lesson_id
FROM waitlist_entries w
JOIN lessons l ON l.id = w.lesson_id
WHERE (w.status = 'invited' AND w.expires_at <= sqlc.arg(now)::timestamptz)
    OR (w.status IN ('waiting', 'invited') AND l.starts_at <= sqlc.arg(now)::timestamptz)
ORDER BY w.lesson_id;
