-- name: CreateLessonNotice :exec
INSERT INTO lesson_notices (id, lesson_id, sender_id, sent_at, recipients) VALUES ($1, $2, $3, $4, $5);

-- name: ListLessonNoticesBySender :many
SELECT * FROM lesson_notices
WHERE sender_id = sqlc.arg(sender_id) AND sent_at >= sqlc.arg(since)
ORDER BY sent_at, id;

-- name: ListLessonNoticesByLesson :many
SELECT * FROM lesson_notices
WHERE lesson_id = sqlc.arg(lesson_id) AND sent_at >= sqlc.arg(since)
ORDER BY sent_at, id;
