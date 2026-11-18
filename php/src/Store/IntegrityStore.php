<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use InvalidArgumentException;
use RuntimeException;
use Symfony\Component\Uid\Uuid;

/** @phpstan-type Finding array{check:string,entity:string,id:string,message:string} */
final readonly class IntegrityStore
{
    public const array CHECKS = ['passes', 'capacity', 'tombstones', 'receipt-counters'];

    private const string UNBALANCED = <<<'SQL'
        SELECT p.id, p.visits_total - p.visits_remaining AS used, count(r.booking_id) AS redeemed
        FROM passes p LEFT JOIN pass_redemptions r ON r.pass_id = p.id
        WHERE p.kind = 'visits'
        GROUP BY p.id, p.visits_total, p.visits_remaining
        HAVING p.visits_total - p.visits_remaining <> count(r.booking_id)
        ORDER BY p.id
        SQL;

    private const string STRAY = <<<'SQL'
        SELECT r.pass_id, r.booking_id, b.lesson_id,
            CASE WHEN b.status = 'canceled' AND NOT b.late THEN 1 ELSE 0 END AS canceled_in_time,
            CASE WHEN l.status = 'canceled' THEN 1 ELSE 0 END AS lesson_canceled
        FROM pass_redemptions r JOIN bookings b ON b.id = r.booking_id JOIN lessons l ON l.id = b.lesson_id
        WHERE (b.status = 'canceled' AND NOT b.late) OR l.status = 'canceled'
        ORDER BY r.booking_id
        SQL;

    private const string OVER_CAPACITY = <<<'SQL'
        WITH booking_totals AS (
            SELECT l.id, l.capacity, count(*) AS active,
                sum(CASE WHEN EXISTS (SELECT 1 FROM booking_events e WHERE e.booking_id = b.id AND e.override)
                    THEN 1 ELSE 0 END) AS overrides
            FROM bookings b JOIN lessons l ON l.id = b.lesson_id
            WHERE b.status <> 'canceled'
            GROUP BY l.id, l.capacity
        )
        SELECT id, capacity, active, overrides FROM booking_totals
        WHERE active - overrides > capacity
        ORDER BY id
        SQL;

    private const string RECEIPT_COUNTERS = <<<'SQL'
        SELECT r.year, coalesce(c.last, 0) AS last, r.highest
        FROM (SELECT year, max(sequence) AS highest FROM receipts GROUP BY year) r
        LEFT JOIN receipt_counters c ON r.year = c.year
        WHERE coalesce(c.last, 0) < r.highest
        ORDER BY r.year
        SQL;

    // Match the reference checks in internal/store/postgres/queries/integrity.sql.
    // A person that was both merged and anonymized follows the merge rules.
    private const string TOMBSTONES = <<<'SQL'
        WITH t AS (
            SELECT id, merged_into IS NOT NULL AS merged,
                CASE WHEN merged_into IS NULL THEN 1 ELSE 0 END AS anonymized, anonymized_at
            FROM persons WHERE merged_into IS NOT NULL OR anonymized_at IS NOT NULL
        )
        SELECT t.id AS person_id, t.anonymized, 'booking' AS kind, 1 AS ord, b.id AS ref
        FROM t JOIN bookings b ON b.person_id = t.id JOIN lessons l ON l.id = b.lesson_id
        WHERE t.merged OR (b.status <> 'canceled' AND l.starts_at > t.anonymized_at)
        UNION ALL
        SELECT t.id, t.anonymized, 'waitlist-entry', 2, w.id
        FROM t JOIN waitlist_entries w ON w.person_id = t.id WHERE w.status IN ('waiting', 'invited')
        UNION ALL
        SELECT t.id, t.anonymized, 'class-member', 3, m.class_id FROM t JOIN class_members m ON m.person_id = t.id
        UNION ALL
        SELECT t.id, t.anonymized, 'guardian', 4, g.dependent_id FROM t JOIN person_guardians g ON g.guardian_id = t.id
        UNION ALL
        SELECT t.id, t.anonymized, 'dependent', 5, g.guardian_id FROM t JOIN person_guardians g ON g.dependent_id = t.id
        UNION ALL
        SELECT t.id, t.anonymized, 'pass', 6, p.id FROM t JOIN passes p ON p.person_id = t.id
        WHERE t.merged OR p.sold_at > t.anonymized_at
        UNION ALL
        SELECT t.id, t.anonymized, 'paid-pass', 7, p.id FROM t JOIN passes p ON p.payer_person_id = t.id
        WHERE t.merged OR p.sold_at > t.anonymized_at
        UNION ALL
        SELECT t.id, t.anonymized, 'staff', 8, s.id FROM t JOIN staff s ON s.person_id = t.id
        UNION ALL
        SELECT t.id, t.anonymized, 'account', 9, a.id FROM t JOIN accounts a ON a.person_id = t.id
        WHERE NOT t.merged OR a.disabled_at IS NULL
        UNION ALL
        SELECT t.id, t.anonymized, 'duplicate-dismissal', 10, d.person_b
        FROM t JOIN person_duplicate_dismissals d ON d.person_a = t.id
        UNION ALL
        SELECT t.id, t.anonymized, 'duplicate-dismissal', 10, d.person_a
        FROM t JOIN person_duplicate_dismissals d ON d.person_b = t.id
        UNION ALL
        SELECT t.id, t.anonymized, 'merged-person', 11, m.id FROM t JOIN persons m ON m.merged_into = t.id WHERE t.merged
        ORDER BY person_id, ord, ref
        SQL;

    private const array REFERENCES = [
        'booking' => 'booking %s',
        'waitlist-entry' => 'open waiting-list entry %s',
        'class-member' => 'membership of class %s',
        'guardian' => 'guardian link to dependent %s',
        'dependent' => 'guardian link from guardian %s',
        'pass' => 'pass %s it owns',
        'paid-pass' => 'pass %s it paid for',
        'staff' => 'staff record %s',
        'account' => 'account %s',
        'duplicate-dismissal' => 'duplicate dismissal with person %s',
        'merged-person' => 'person %s merged into it',
    ];

    public function __construct(private Connection $db) {}

    /**
     * @param list<string> $requested
     * @return list<value-of<self::CHECKS>>
     */
    public static function select(array $requested): array
    {
        if ($requested === []) {
            return self::CHECKS;
        }

        $unknown = array_diff($requested, self::CHECKS);
        if ($unknown !== []) {
            $name = json_encode(reset($unknown), JSON_INVALID_UTF8_SUBSTITUTE | JSON_THROW_ON_ERROR);
            throw new InvalidArgumentException("unknown check $name; the checks are " . implode(', ', self::CHECKS));
        }

        return array_values(array_intersect(self::CHECKS, $requested));
    }

    /**
     * Run in the caller's transaction. A copy can inspect its uncommitted
     * target rows, and the CLI checks a read-only snapshot.
     *
     * @param list<string> $checks
     * @return list<Finding>
     */
    public function findings(array $checks = []): array
    {
        $findings = [];
        foreach (self::select($checks) as $check) {
            array_push($findings, ...match ($check) {
                'passes' => $this->passes(),
                'capacity' => $this->capacity(),
                'tombstones' => $this->tombstones(),
                'receipt-counters' => $this->receiptCounters(),
            });
        }
        return $findings;
    }

    /** @return list<Finding> */
    private function passes(): array
    {
        $findings = [];
        foreach ($this->db->iterateAssociative(self::UNBALANCED) as $row) {
            $used = self::number($row['used']);
            $redeemed = self::number($row['redeemed']);
            $findings[] = self::finding('passes', 'pass', $this->id($row['id']), "$used of its visits are used, but it paid for $redeemed bookings");
        }
        foreach ($this->db->iterateAssociative(self::STRAY) as $row) {
            $why = [];
            if (self::number($row['canceled_in_time']) === 1) {
                $why[] = 'the booking was canceled in time';
            }
            if (self::number($row['lesson_canceled']) === 1) {
                $why[] = 'the school canceled lesson ' . $this->id($row['lesson_id']);
            }
            $pass = $this->id($row['pass_id']);
            $message = "pass $pass paid for it, but it costs no visit: " . implode(', and ', $why);
            $findings[] = self::finding('passes', 'booking', $this->id($row['booking_id']), $message);
        }

        return $findings;
    }

    /** @return list<Finding> */
    private function capacity(): array
    {
        $findings = [];
        foreach ($this->db->iterateAssociative(self::OVER_CAPACITY) as $row) {
            $active = self::number($row['active']);
            $capacity = self::number($row['capacity']);
            $overrides = self::number($row['overrides']);
            $message = "$active active bookings for $capacity places, and only $overrides of them were booked beyond the capacity on purpose";
            $findings[] = self::finding('capacity', 'lesson', $this->id($row['id']), $message);
        }

        return $findings;
    }

    /** @return list<Finding> */
    private function tombstones(): array
    {
        $findings = [];
        foreach ($this->db->iterateAssociative(self::TOMBSTONES) as $row) {
            $kind = $row['kind'];
            if (!is_string($kind) || !isset(self::REFERENCES[$kind])) {
                throw new RuntimeException('Unexpected tombstone reference in the database');
            }
            $ref = sprintf(self::REFERENCES[$kind], $this->id($row['ref']));
            $status = self::number($row['anonymized']) === 1 ? 'anonymized' : 'merged';
            $message = "the person is $status, but $ref still refers to it";
            $findings[] = self::finding('tombstones', 'person', $this->id($row['person_id']), $message);
        }

        return $findings;
    }

    /** @return list<Finding> */
    private function receiptCounters(): array
    {
        $findings = [];
        foreach ($this->db->iterateAssociative(self::RECEIPT_COUNTERS) as $row) {
            $year = (string) self::number($row['year']);
            $last = self::number($row['last']);
            $highest = self::number($row['highest']);
            $message = "the counter is at $last, but receipt number $highest was issued: the next receipt would repeat a number";
            $findings[] = self::finding('receipt-counters', 'receipt-year', $year, $message);
        }

        return $findings;
    }

    /** @return Finding */
    private static function finding(string $check, string $entity, string $id, string $message): array
    {
        return ['check' => $check, 'entity' => $entity, 'id' => $id, 'message' => $message];
    }

    private function id(mixed $id): string
    {
        if (!is_string($id)) {
            throw new RuntimeException('Unexpected ID in the database');
        }

        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform ? $id : Uuid::fromBinary($id)->toRfc4122();
    }

    private static function number(mixed $value): int
    {
        if (!is_numeric($value)) {
            throw new RuntimeException('Unexpected count in the database');
        }

        return (int) $value;
    }
}
