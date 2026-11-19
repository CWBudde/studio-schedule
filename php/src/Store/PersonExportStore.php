<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\Instant;
use Symfony\Component\Uid\Uuid;

/** @phpstan-type Column string|array{string,'id'|'instant'|'integer'|'boolean'|'clock'} */
final readonly class PersonExportStore
{
    private const array PERSON = ['id' => ['id', 'id'], 'given_name' => 'givenName', 'family_name' => 'familyName'];
    private const array LESSON = [
        'lesson_id' => ['id', 'id'], 'starts_at' => ['startsAt', 'instant'], 'ends_at' => ['endsAt', 'instant'], 'course_type' => 'courseType',
    ];
    private const string LESSON_JOIN = ' JOIN lessons l ON l.id = entry.lesson_id LEFT JOIN course_types ct ON ct.id = l.course_type_id';

    public function __construct(private Connection $db) {}

    /** @return array<string,mixed>|null */
    public function person(string $id): ?array
    {
        $row = $this->db->fetchAssociative(
            'SELECT id, given_name, family_name, email, phone, birth_date, notes, member_no, merged_into, anonymized_at'
            . ' FROM persons WHERE id = ' . $this->key(),
            [$this->value($id)],
        );

        return $row === false ? null : $this->fields($row, self::PERSON, [
            'email' => 'email', 'phone' => 'phone', 'birth_date' => 'birthDate', 'notes' => 'notes', 'member_no' => 'memberNo',
            'merged_into' => ['mergedIntoId', 'id'], 'anonymized_at' => ['anonymizedAt', 'instant'],
        ]);
    }

    /** @return array{guardians:list<array<string,mixed>>,dependents:list<array<string,mixed>>,mergedPersons:list<array<string,mixed>>} */
    public function familyLinks(string $id): array
    {
        $order = $this->isPostgres() ? 'p.family_name, p.given_name, p.id' : 'CAST(p.family_name AS BINARY), CAST(p.given_name AS BINARY), p.id';
        $key = $this->key();
        $select = 'SELECT p.id, p.given_name, p.family_name FROM persons p';
        $params = [$this->value($id)];

        return [
            'guardians' => $this->rows("$select JOIN person_guardians g ON p.id = g.guardian_id WHERE g.dependent_id = $key ORDER BY $order", $params, self::PERSON),
            'dependents' => $this->rows("$select JOIN person_guardians g ON p.id = g.dependent_id WHERE g.guardian_id = $key ORDER BY $order", $params, self::PERSON),
            'mergedPersons' => $this->rows("$select WHERE p.merged_into = $key ORDER BY $order", $params, self::PERSON),
        ];
    }

    /** @return array<string,mixed> */
    public function accountData(string $personId): array
    {
        $key = $this->key();
        $row = $this->db->fetchAssociative(
            'SELECT id, email, role, locale, NULLIF(visible_days, 0) AS visible_days, confirmed_at, disabled_at '
            . "FROM accounts WHERE person_id = $key",
            [$this->value($personId)],
        );
        if ($row === false) {
            return [];
        }
        $params = [$this->value($this->id(self::text($row['id'])))];

        return [
            'account' => $this->fields($row, ['id' => ['id', 'id'], 'email' => 'email', 'role' => 'role'], [
                'locale' => 'locale', 'visible_days' => ['visibleDays', 'integer'],
                'confirmed_at' => ['confirmedAt', 'instant'], 'disabled_at' => ['disabledAt', 'instant'],
            ]),
            'calendarFeeds' => $this->rows(
                "SELECT id, scope, location_id, created_at, last_used_at FROM calendar_feeds WHERE account_id = $key AND revoked_at IS NULL ORDER BY created_at, id",
                $params,
                ['id' => ['id', 'id'], 'scope' => 'scope', 'created_at' => ['createdAt', 'instant']],
                ['location_id' => ['locationId', 'id'], 'last_used_at' => ['lastUsedAt', 'instant']],
            ),
            'pushDevices' => $this->rows(
                "SELECT id, device_label, created_at, last_used_at FROM push_subscriptions WHERE account_id = $key ORDER BY created_at, id",
                $params,
                ['id' => ['id', 'id'], 'created_at' => ['createdAt', 'instant'], 'last_used_at' => ['lastUsedAt', 'instant']],
                ['device_label' => 'deviceLabel'],
            ),
            'dismissedAnnouncements' => array_map(static fn(array $announcement): array => $announcement + ['publishAt' => '0001-01-01T00:00:00Z'], $this->rows(
                'SELECT a.id, a.title, a.publish_at FROM announcement_dismissals d '
                . "JOIN announcements a ON a.id = d.announcement_id WHERE d.account_id = $key "
                . 'ORDER BY (a.publish_at IS NULL) DESC, a.publish_at DESC, a.id DESC',
                $params,
                ['id' => ['id', 'id'], 'title' => 'title'],
                ['publish_at' => ['publishAt', 'instant']],
            )),
        ];
    }

    /** @return list<array<string,mixed>> */
    public function classMemberships(string $id): array
    {
        return $this->rows(
            'SELECT c.id, c.term_id, t.name AS term_name, ct.name AS course_type, c.weekday, c.start_time, c.retired, m.since '
            . 'FROM class_members m JOIN classes c ON c.id = m.class_id JOIN terms t ON t.id = c.term_id '
            . 'JOIN course_types ct ON ct.id = c.course_type_id WHERE m.person_id = ' . $this->key()
            . ' ORDER BY t.starts_on, c.weekday, EXTRACT(HOUR FROM c.start_time), EXTRACT(MINUTE FROM c.start_time), c.id',
            [$this->value($id)],
            [
                'id' => ['classId', 'id'], 'term_id' => ['termId', 'id'], 'term_name' => 'termName', 'course_type' => 'courseTypeName',
                'weekday' => ['weekday', 'integer'], 'start_time' => ['startTime', 'clock'], 'retired' => ['retired', 'boolean'],
            ],
            ['since' => 'since'],
        );
    }

    /** @return list<array<string,mixed>> */
    public function bookings(string $id): array
    {
        $rows = $this->db->fetchAllAssociative(
            'SELECT entry.id, entry.status, entry.source, entry.trial, entry.trial_note, entry.booked_at, entry.canceled_at, entry.late, '
            . "entry.lesson_id, l.starts_at, l.ends_at, COALESCE(ct.name, '') AS course_type FROM bookings entry"
            . self::LESSON_JOIN . ' WHERE entry.person_id = ' . $this->key() . ' ORDER BY l.starts_at, l.id, entry.booked_at, entry.id',
            [$this->value($id)],
        );

        return array_map(fn(array $row): array => $this->fields($row, [
            'id' => ['id', 'id'], 'status' => 'status', 'source' => 'source', 'trial' => ['trial', 'boolean'],
            'booked_at' => ['bookedAt', 'instant'], 'late' => ['late', 'boolean'],
        ], ['trial_note' => 'trialNote', 'canceled_at' => ['canceledAt', 'instant']]) + ['lesson' => $this->fields($row, self::LESSON)], $rows);
    }

    /** @return list<array<string,mixed>> */
    public function waitlistEntries(string $id): array
    {
        $rows = $this->db->fetchAllAssociative(
            'SELECT entry.id, entry.status, entry.joined_at, entry.invited_at, '
            . "entry.lesson_id, l.starts_at, l.ends_at, COALESCE(ct.name, '') AS course_type FROM waitlist_entries entry"
            . self::LESSON_JOIN . ' WHERE entry.person_id = ' . $this->key() . ' ORDER BY entry.joined_at, entry.id',
            [$this->value($id)],
        );

        return array_map(fn(array $row): array => $this->fields($row, [
            'id' => ['id', 'id'], 'status' => 'status', 'joined_at' => ['joinedAt', 'instant'],
        ], ['invited_at' => ['invitedAt', 'instant']]) + ['lesson' => $this->fields($row, self::LESSON)], $rows);
    }

    /** @return list<array<string,mixed>> */
    public function eventRegistrations(string $id): array
    {
        return $this->rows(
            'SELECT r.id, e.title, r.status, r.payment, r.price, fee.number AS receipt, refund.number AS cancellation, r.created_at, r.canceled_at '
            . 'FROM event_registrations r JOIN events e ON e.id = r.event_id '
            . 'LEFT JOIN receipts fee ON fee.id = r.receipt_id LEFT JOIN receipts refund ON refund.id = r.cancellation_receipt_id '
            . 'WHERE r.person_id = ' . $this->key() . ' ORDER BY r.created_at, r.id',
            [$this->value($id)],
            [
                'id' => ['id', 'id'], 'title' => 'title', 'status' => 'status', 'payment' => 'payment',
                'price' => ['price', 'integer'], 'created_at' => ['createdAt', 'instant'],
            ],
            ['receipt' => 'receiptNumber', 'cancellation' => 'cancellationReceiptNumber', 'canceled_at' => ['canceledAt', 'instant']],
        );
    }

    /** @return list<array<string,mixed>> */
    public function passes(string $id): array
    {
        $key = $this->key();
        $params = [$this->value($id)];
        $rows = $this->db->fetchAllAssociative(
            'SELECT p.id, t.name, p.kind, COALESCE(p.visits_total, 0) AS visits_total, COALESCE(p.visits_remaining, 0) AS visits_remaining, '
            . 'p.valid_from, p.valid_until, p.status, p.sold_at, p.price_paid, p.note, p.voided_at FROM passes p '
            . "JOIN pass_types t ON t.id = p.pass_type_id WHERE p.person_id = $key ORDER BY p.sold_at, p.id",
            $params,
        );
        if ($rows === []) {
            return [];
        }
        $redemptions = [];
        foreach ($this->db->fetchAllAssociative(
            'SELECT r.pass_id, r.booking_id, r.at FROM pass_redemptions r JOIN passes p ON p.id = r.pass_id WHERE p.person_id = '
            . $key . ' ORDER BY r.pass_id, r.at, r.booking_id',
            $params,
        ) as $row) {
            $redemptions[$this->id(self::text($row['pass_id']))][] = $this->fields($row, ['booking_id' => ['bookingId', 'id'], 'at' => ['at', 'instant']]);
        }

        return array_map(fn(array $row): array => $this->fields($row, [
            'id' => ['id', 'id'], 'name' => 'type', 'kind' => 'kind', 'visits_total' => ['visitsTotal', 'integer'],
            'visits_remaining' => ['visitsRemaining', 'integer'], 'valid_from' => 'validFrom', 'status' => 'status',
            'sold_at' => ['soldAt', 'instant'], 'price_paid' => ['pricePaid', 'integer'],
        ], ['valid_until' => 'validUntil', 'note' => 'note', 'voided_at' => ['voidedAt', 'instant']])
            + ['redemptions' => $redemptions[$this->id(self::text($row['id']))] ?? []], $rows);
    }

    /** @return list<array<string,mixed>> */
    public function receipts(string $id): array
    {
        $key = $this->key();
        $value = $this->value($id);

        return $this->rows(
            'SELECT r.id, r.number, r.kind, r.issue_date, r.recipient_name, r.currency, r.gross, '
            . 'EXISTS (SELECT 1 FROM receipts c WHERE c.cancels_receipt_id = r.id) AS canceled FROM receipts r '
            . "WHERE r.payer_person_id IN (SELECT p.id FROM persons p WHERE p.id = $key OR p.merged_into = $key) "
            . 'ORDER BY r.year DESC, r.sequence DESC, r.id',
            [$value, $value],
            [
                'id' => ['id', 'id'], 'number' => 'number', 'kind' => 'kind', 'issue_date' => 'issueDate', 'recipient_name' => 'recipientName',
                'currency' => 'currency', 'gross' => ['gross', 'integer'], 'canceled' => ['canceled', 'boolean'],
            ],
        );
    }

    /**
     * @param list<string> $params
     * @param array<string,Column> $required
     * @param array<string,Column> $optional
     * @return list<array<string,mixed>>
     */
    private function rows(string $sql, array $params, array $required, array $optional = []): array
    {
        return array_map(fn(array $row): array => $this->fields($row, $required, $optional), $this->db->fetchAllAssociative($sql, $params));
    }

    /**
     * @param array<string,mixed> $row
     * @param array<string,Column> $required
     * @param array<string,Column> $optional
     * @return array<string,mixed>
     */
    private function fields(#[SensitiveParameter] array $row, array $required, array $optional = []): array
    {
        $out = [];
        foreach ($required + $optional as $column => $entry) {
            $value = $row[$column];
            if (isset($optional[$column]) && ($value === null || $value === '')) {
                continue;
            }
            [$name, $type] = is_string($entry) ? [$entry, 'text'] : $entry;
            $out[$name] = match ($type) {
                'id' => $this->id(self::text($value)),
                'instant' => self::instant(self::text($value)),
                'integer' => is_numeric($value) ? (int) $value : throw new RuntimeException('Expected an export integer'),
                'boolean' => in_array($value, [true, 1, '1', 't'], true),
                'clock' => substr(self::text($value), 0, 5),
                'text' => self::text($value),
            };
        }

        return $out;
    }

    private static function instant(string $value): string
    {
        if (str_starts_with($value, '0001-') && str_ends_with($value, ' BC')) {
            $value = '0000' . substr($value, 4, -3);
        }

        return Instant::format(new DateTimeImmutable($value, new DateTimeZone('UTC')));
    }

    private function id(string $value): string
    {
        return $this->isPostgres() ? $value : Uuid::fromBinary($value)->toRfc4122();
    }

    private function key(): string
    {
        return $this->isPostgres() ? '?' : 'UNHEX(?)';
    }

    private function value(string $id): string
    {
        return $this->isPostgres() ? $id : str_replace('-', '', $id);
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private static function text(mixed $value): string
    {
        return is_string($value) ? $value : throw new RuntimeException('Expected export text');
    }
}
