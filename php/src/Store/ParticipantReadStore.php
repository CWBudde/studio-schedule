<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Service\ParticipantReadPersistence;
use Symfony\Component\Uid\Uuid;

/** @phpstan-import-type RosterBooking from ParticipantReadPersistence */
final readonly class ParticipantReadStore implements ParticipantReadPersistence
{
    public function __construct(private Connection $db) {}

    public function read(Closure $read): mixed
    {
        return Database::inReadSnapshot($this->db, $read);
    }

    public function lesson(string $id, ?string $staffPerson): ?array
    {
        $sql = 'SELECT l.starts_at, l.ends_at, l.status, l.event_id, COALESCE(e.capacity, l.capacity) AS capacity, s.timezone, s.redemption_mode '
            . 'FROM lessons l JOIN settings s ON s.id = TRUE LEFT JOIN events e ON e.id = l.event_id WHERE l.id = ?';
        $params = [$this->encodeId($id)];
        if ($staffPerson !== null) {
            $sql .= ' AND EXISTS (SELECT 1 FROM staff st WHERE st.person_id = ? AND st.active = TRUE '
                . 'AND (st.id = l.instructor_id OR st.id = l.accompanist_id))';
            $params[] = $this->encodeId($staffPerson);
        }
        $row = $this->db->fetchAssociative($sql, $params);
        if ($row === false) {
            return null;
        }

        return [
            'startsAt' => self::instant($row['starts_at']), 'endsAt' => self::instant($row['ends_at']),
            'status' => self::text($row['status']), 'eventId' => $this->optionalId($row['event_id']),
            'capacity' => self::integer($row['capacity']), 'timezone' => self::text($row['timezone']),
            'mode' => self::text($row['redemption_mode']),
        ];
    }

    public function bookings(string $lesson): array
    {
        $order = $this->isPostgres() ? 'gp.family_name, gp.given_name, gp.id'
            : 'CAST(gp.family_name AS BINARY), CAST(gp.given_name AS BINARY), gp.id';
        $guardianEmail = 'SELECT ga.email FROM person_guardians g JOIN persons gp ON gp.id = g.guardian_id '
            . 'JOIN accounts ga ON ga.person_id = gp.id AND ga.disabled_at IS NULL AND ga.confirmed_at IS NOT NULL '
            . "WHERE g.dependent_id = p.id ORDER BY $order LIMIT 1";
        $guardianPhone = 'SELECT gp.phone FROM person_guardians g JOIN persons gp ON gp.id = g.guardian_id '
            . "WHERE g.dependent_id = p.id AND NULLIF(gp.phone, '') IS NOT NULL ORDER BY $order LIMIT 1";
        $rows = $this->db->fetchAllAssociative(
            "SELECT b.id, b.person_id, CASE WHEN b.person_id IS NULL THEN b.guest_name ELSE CONCAT(p.given_name, ' ', p.family_name) END AS name, "
            . 'p.anonymized_at, b.status, b.source, b.trial, b.trial_note, b.late, b.booked_at, b.canceled_at, '
            . 'CASE WHEN b.person_id IS NULL THEN b.guest_email WHEN a.disabled_at IS NOT NULL THEN NULL '
            . "WHEN a.confirmed_at IS NOT NULL THEN a.email ELSE COALESCE(NULLIF(p.email, ''), ($guardianEmail)) END AS contact_email, "
            . "COALESCE(NULLIF(p.phone, ''), ($guardianPhone)) AS contact_phone, "
            . 'ps.id AS pass_id, pt.name AS pass_name, ps.kind AS pass_kind, ps.visits_remaining, ps.valid_until '
            . 'FROM bookings b LEFT JOIN persons p ON p.id = b.person_id LEFT JOIN accounts a ON a.person_id = b.person_id '
            . 'LEFT JOIN pass_redemptions pr ON pr.booking_id = b.id AND b.person_id IS NOT NULL '
            . 'LEFT JOIN passes ps ON ps.id = pr.pass_id LEFT JOIN pass_types pt ON pt.id = ps.pass_type_id '
            . 'WHERE b.lesson_id = ? ORDER BY b.booked_at, b.id',
            [$this->encodeId($lesson)],
        );

        return array_map($this->booking(...), $rows);
    }

    /**
     * @param array<string, mixed> $row
     * @return RosterBooking
     */
    private function booking(array $row): array
    {
        $pass = $row['pass_id'] === null ? null : [
            'id' => $this->id($row['pass_id']), 'name' => self::text($row['pass_name']), 'kind' => self::text($row['pass_kind']),
            'visitsRemaining' => $row['visits_remaining'] === null ? 0 : self::integer($row['visits_remaining']),
        ];
        if ($pass !== null && $row['valid_until'] !== null) {
            $pass['validUntil'] = self::text($row['valid_until']);
        }

        return [
            'bookingId' => $this->id($row['id']), 'personId' => $this->optionalId($row['person_id']),
            'name' => self::text($row['name']), 'guest' => $row['person_id'] === null,
            'anonymized' => $row['anonymized_at'] === null ? null : true,
            'contactEmail' => self::optionalText($row['contact_email']), 'contactPhone' => self::optionalText($row['contact_phone']),
            'status' => self::text($row['status']), 'source' => self::text($row['source']),
            'trial' => self::boolean($row['trial']), 'trialNote' => self::optionalText($row['trial_note']),
            'late' => self::boolean($row['late']), 'bookedAt' => Instant::format(self::instant($row['booked_at'])),
            'canceledAt' => $row['canceled_at'] === null ? null : Instant::format(self::instant($row['canceled_at'])),
            'pass' => $pass,
        ];
    }

    public function waitlist(string $lesson): array
    {
        $rows = $this->db->fetchAllAssociative(
            "SELECT w.id, w.person_id, CONCAT(p.given_name, ' ', p.family_name) AS name, p.anonymized_at, "
            . 'w.status, w.stay_on_list, w.joined_at, w.expires_at FROM waitlist_entries w '
            . 'JOIN persons p ON p.id = w.person_id WHERE w.lesson_id = ? ORDER BY w.joined_at, w.id',
            [$this->encodeId($lesson)],
        );

        return array_map(fn(array $row): array => [
            'entryId' => $this->id($row['id']), 'personId' => $this->id($row['person_id']), 'name' => self::text($row['name']),
            'anonymized' => $row['anonymized_at'] === null ? null : true, 'status' => self::text($row['status']),
            'stayOnList' => self::boolean($row['stay_on_list']), 'joinedAt' => Instant::format(self::instant($row['joined_at'])),
            'expiresAt' => $row['expires_at'] === null ? null : self::instant($row['expires_at']),
        ], $rows);
    }

    public function registrations(string $event): array
    {
        $rows = $this->db->fetchAllAssociative(
            "SELECT r.id, r.person_id, CASE WHEN r.person_id IS NULL THEN r.guest_name ELSE CONCAT(p.given_name, ' ', p.family_name) END AS name, "
            . 'p.anonymized_at, r.status, r.expires_at FROM event_registrations r '
            . 'LEFT JOIN persons p ON p.id = r.person_id WHERE r.event_id = ? ORDER BY r.created_at, r.id',
            [$this->encodeId($event)],
        );

        return array_map(fn(array $row): array => [
            'id' => $this->id($row['id']), 'name' => self::text($row['name']), 'guest' => $row['person_id'] === null,
            'anonymized' => $row['anonymized_at'] === null ? null : true, 'status' => self::text($row['status']),
            'expiresAt' => $row['expires_at'] === null ? null : self::instant($row['expires_at']),
        ], $rows);
    }

    public function bookingEvents(string $booking): ?array
    {
        $rows = $this->db->fetchAllAssociative(
            "SELECT e.id, e.at, e.status, e.late, e.override, e.note, e.actor_account_id, CONCAT(p.given_name, ' ', p.family_name) AS name, a.email "
            . 'FROM bookings b LEFT JOIN booking_events e ON e.booking_id = b.id '
            . 'LEFT JOIN accounts a ON a.id = e.actor_account_id LEFT JOIN persons p ON p.id = a.person_id '
            . 'WHERE b.id = ? ORDER BY e.at, e.id',
            [$this->encodeId($booking)],
        );
        if ($rows === []) {
            return null;
        }
        $events = [];
        foreach ($rows as $row) {
            if ($row['id'] === null) {
                continue;
            }
            $event = [
                'id' => $this->id($row['id']), 'at' => Instant::format(self::instant($row['at'])), 'status' => self::text($row['status']),
                'late' => self::boolean($row['late']), 'override' => self::boolean($row['override']),
            ];
            if ($row['note'] !== null && $row['note'] !== '') {
                $event['note'] = self::text($row['note']);
            }
            if ($row['actor_account_id'] !== null) {
                $event['actor'] = ['accountId' => $this->id($row['actor_account_id']), 'name' => self::text($row['name']), 'email' => self::text($row['email'])];
            }
            $events[] = $event;
        }

        return $events;
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private function id(mixed $value): string
    {
        $raw = self::text($value);

        return $this->isPostgres() ? $raw : Uuid::fromBinary($raw)->toRfc4122();
    }

    private function optionalId(mixed $value): ?string
    {
        return $value === null ? null : $this->id($value);
    }

    private function encodeId(string $value): string
    {
        return $this->isPostgres() ? $value : Uuid::fromString($value)->toBinary();
    }

    private static function boolean(mixed $value): bool
    {
        return in_array($value, [true, 1, '1', 't'], true);
    }

    private static function integer(mixed $value): int
    {
        return is_numeric($value) ? (int) $value : throw new RuntimeException('Expected a participant integer column');
    }

    private static function optionalText(mixed $value): ?string
    {
        return $value === null ? null : self::text($value);
    }

    private static function text(mixed $value): string
    {
        return is_string($value) ? $value : throw new RuntimeException('Expected a participant text column');
    }

    private static function instant(mixed $value): DateTimeImmutable
    {
        return Database::instant(self::text($value));
    }
}
