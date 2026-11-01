-- name: ListUnbalancedPasses :many
SELECT p.id, (p.visits_total - p.visits_remaining)::integer AS used, count(r.booking_id)::integer AS redeemed
FROM passes p
LEFT JOIN pass_redemptions r ON r.pass_id = p.id
WHERE p.kind = 'visits'
GROUP BY p.id
HAVING p.visits_total - p.visits_remaining <> count(r.booking_id)
ORDER BY p.id;

-- name: ListStrayRedemptions :many
SELECT r.pass_id, r.booking_id, r.at, b.lesson_id,
    (b.status = 'canceled' AND NOT b.late)::boolean AS canceled_in_time,
    (l.status = 'canceled')::boolean AS lesson_canceled
FROM pass_redemptions r
JOIN bookings b ON b.id = r.booking_id
JOIN lessons l ON l.id = b.lesson_id
WHERE (b.status = 'canceled' AND NOT b.late) OR l.status = 'canceled'
ORDER BY r.booking_id;

-- name: ListLessonsOverCapacity :many
SELECT l.id, l.capacity, count(*)::integer AS active,
    count(*) FILTER (
        WHERE EXISTS (SELECT 1 FROM booking_events e WHERE e.booking_id = b.id AND e.override)
    )::integer AS overrides
FROM bookings b
JOIN lessons l ON l.id = b.lesson_id
WHERE b.status <> 'canceled'
GROUP BY l.id
HAVING count(*) > l.capacity
ORDER BY l.id;

-- name: ListReceiptCounterStates :many
SELECT coalesce(c.year, r.year)::integer AS year, coalesce(c.last, 0)::integer AS last,
    coalesce(r.highest, 0)::integer AS highest
FROM receipt_counters c
FULL JOIN (SELECT year, max(sequence) AS highest FROM receipts GROUP BY year) r ON r.year = c.year
ORDER BY 1;

-- name: ListTombstoneReferences :many
-- The rules of a merged person apply to one that is also anonymized.
WITH t AS (
    SELECT id, merged_into IS NOT NULL AS merged, anonymized_at
    FROM persons
    WHERE merged_into IS NOT NULL OR anonymized_at IS NOT NULL
)
SELECT t.id AS person_id, (NOT t.merged)::boolean AS anonymized, 'booking'::text AS kind, 1 AS ord, b.id AS ref
FROM t
JOIN bookings b ON b.person_id = t.id
JOIN lessons l ON l.id = b.lesson_id
WHERE t.merged OR (b.status <> 'canceled' AND l.starts_at > t.anonymized_at)
UNION ALL
SELECT t.id, NOT t.merged, 'waitlist-entry', 2, w.id
FROM t JOIN waitlist_entries w ON w.person_id = t.id
WHERE w.status IN ('waiting', 'invited')
UNION ALL
SELECT t.id, NOT t.merged, 'class-member', 3, m.class_id
FROM t JOIN class_members m ON m.person_id = t.id
UNION ALL
SELECT t.id, NOT t.merged, 'guardian', 4, g.dependent_id
FROM t JOIN person_guardians g ON g.guardian_id = t.id
UNION ALL
SELECT t.id, NOT t.merged, 'dependent', 5, g.guardian_id
FROM t JOIN person_guardians g ON g.dependent_id = t.id
UNION ALL
SELECT t.id, NOT t.merged, 'pass', 6, p.id
FROM t JOIN passes p ON p.person_id = t.id
WHERE t.merged OR p.sold_at > t.anonymized_at
UNION ALL
SELECT t.id, NOT t.merged, 'paid-pass', 7, p.id
FROM t JOIN passes p ON p.payer_person_id = t.id
WHERE t.merged OR p.sold_at > t.anonymized_at
UNION ALL
SELECT t.id, NOT t.merged, 'staff', 8, s.id
FROM t JOIN staff s ON s.person_id = t.id
UNION ALL
SELECT t.id, NOT t.merged, 'account', 9, a.id
FROM t JOIN accounts a ON a.person_id = t.id
WHERE NOT t.merged OR a.disabled_at IS NULL
UNION ALL
SELECT t.id, NOT t.merged, 'duplicate-dismissal', 10, d.person_b
FROM t JOIN person_duplicate_dismissals d ON d.person_a = t.id
UNION ALL
SELECT t.id, NOT t.merged, 'duplicate-dismissal', 10, d.person_a
FROM t JOIN person_duplicate_dismissals d ON d.person_b = t.id
UNION ALL
SELECT t.id, NOT t.merged, 'merged-person', 11, m.id
FROM t JOIN persons m ON m.merged_into = t.id
WHERE t.merged
ORDER BY person_id, ord, ref;
