<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Domain\Date;
use StudioSchedule\Service\SettingsNotFound;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-type Bookings array{bookings:int,attended:int,noShows:int,lateCancellations:int,trials:int,guests:int}
 * @phpstan-type ClassSlot array{weekday:int,startTime:string}
 * @phpstan-type SalesDay array{date:string,passesSold:int,salesGross:int,eventFees:int}
 */
final readonly class StatisticsStore
{
    private const string RANGE = 'l.starts_at >= ? AND l.starts_at <= ?';
    private const string BOOKABLE = self::RANGE . " AND l.event_id IS NULL AND l.kind <> 'rental' AND l.status <> 'canceled'";

    public function __construct(private Connection $db) {}

    /** @return array{timezone:string,currency:string} */
    public function school(): array
    {
        $row = $this->db->fetchAssociative('SELECT timezone, currency FROM settings WHERE id = TRUE');
        if ($row === false) {
            throw new SettingsNotFound();
        }

        return ['timezone' => self::text($row['timezone']), 'currency' => self::text($row['currency'])];
    }

    /** @return array{id:string,name:string,start:string}|null */
    public function term(Date $today): ?array
    {
        $row = $this->db->fetchAssociative(
            'SELECT id, name, starts_on FROM terms WHERE starts_on <= ? AND ends_on >= ? ORDER BY starts_on DESC, id DESC LIMIT 1',
            [(string) $today, (string) $today],
        );

        return $row === false ? null : ['id' => $this->id($row['id']), 'name' => self::text($row['name']), 'start' => self::text($row['starts_on'])];
    }

    /** @return array<string,ClassSlot> */
    public function classSlots(DateTimeImmutable $start, DateTimeImmutable $end): array
    {
        $rows = $this->db->fetchAllAssociative(
            'SELECT DISTINCT c.id, c.weekday, c.start_time FROM lessons l JOIN classes c ON c.id = l.class_id WHERE ' . self::BOOKABLE,
            $this->bounds($start, $end),
        );
        $slots = [];
        foreach ($rows as $row) {
            $slots[$this->id($row['id'])] = ['weekday' => self::integer($row['weekday']), 'startTime' => substr(self::text($row['start_time']), 0, 5)];
        }

        return $slots;
    }

    /** @return array<string,Bookings> */
    public function bookings(DateTimeImmutable $start, DateTimeImmutable $end): array
    {
        $select = <<<'SQL'
            SELECT l.id,
                SUM(CASE WHEN b.status <> 'canceled' THEN 1 ELSE 0 END) AS bookings,
                SUM(CASE WHEN b.status = 'attended' THEN 1 ELSE 0 END) AS attended,
                SUM(CASE WHEN b.status = 'no_show' THEN 1 ELSE 0 END) AS no_shows,
                SUM(CASE WHEN b.status = 'canceled' AND b.late THEN 1 ELSE 0 END) AS late_cancellations,
                SUM(CASE WHEN b.status <> 'canceled' AND b.trial THEN 1 ELSE 0 END) AS trials,
                SUM(CASE WHEN b.status <> 'canceled' AND b.person_id IS NULL THEN 1 ELSE 0 END) AS guests
            FROM lessons l JOIN bookings b ON b.lesson_id = l.id
            WHERE
            SQL;
        $rows = $this->db->fetchAllAssociative($select . ' ' . self::BOOKABLE . ' GROUP BY l.id', $this->bounds($start, $end));
        $totals = [];
        foreach ($rows as $row) {
            $totals[$this->id($row['id'])] = [
                'bookings' => self::integer($row['bookings']), 'attended' => self::integer($row['attended']),
                'noShows' => self::integer($row['no_shows']), 'lateCancellations' => self::integer($row['late_cancellations']),
                'trials' => self::integer($row['trials']), 'guests' => self::integer($row['guests']),
            ];
        }

        return $totals;
    }

    /** @return array<string,int> */
    public function waitlist(DateTimeImmutable $start, DateTimeImmutable $end): array
    {
        $rows = $this->db->fetchAllAssociative(
            'SELECT l.id, COUNT(*) AS entries FROM lessons l JOIN waitlist_entries w ON w.lesson_id = l.id WHERE ' . self::BOOKABLE . ' GROUP BY l.id',
            $this->bounds($start, $end),
        );
        $counts = [];
        foreach ($rows as $row) {
            $counts[$this->id($row['id'])] = self::integer($row['entries']);
        }

        return $counts;
    }

    public function activeMembers(DateTimeImmutable $start, DateTimeImmutable $end): int
    {
        return self::integer($this->db->fetchOne(
            'SELECT COUNT(DISTINCT b.person_id) FROM lessons l JOIN bookings b ON b.lesson_id = l.id WHERE '
            . self::BOOKABLE . " AND b.status <> 'canceled'",
            $this->bounds($start, $end),
        ));
    }

    /** @return list<array{personId:string,givenName:string,familyName:string,bookings:int,attended:int,lateCancellations:int}> */
    public function topParticipants(DateTimeImmutable $start, DateTimeImmutable $end): array
    {
        $select = <<<'SQL'
            SELECT p.id, p.given_name, p.family_name,
                SUM(CASE WHEN b.status <> 'canceled' THEN 1 ELSE 0 END) AS bookings,
                SUM(CASE WHEN b.status = 'attended' THEN 1 ELSE 0 END) AS attended,
                SUM(CASE WHEN b.status = 'canceled' AND b.late THEN 1 ELSE 0 END) AS late_cancellations
            FROM lessons l JOIN bookings b ON b.lesson_id = l.id JOIN persons p ON p.id = b.person_id
            WHERE p.merged_into IS NULL AND p.anonymized_at IS NULL AND
            SQL;
        $names = $this->isPostgres()
            ? 'p.family_name COLLATE "C", p.given_name COLLATE "C"'
            : 'CAST(p.family_name AS BINARY), CAST(p.given_name AS BINARY)';
        $rows = $this->db->fetchAllAssociative(
            $select . ' ' . self::BOOKABLE
            . " GROUP BY p.id, p.given_name, p.family_name HAVING COUNT(CASE WHEN b.status <> 'canceled' THEN 1 END) > 0"
            . " ORDER BY bookings DESC, $names, p.id LIMIT 20",
            $this->bounds($start, $end),
        );

        return array_map(fn(array $row): array => [
            'personId' => $this->id($row['id']), 'givenName' => self::text($row['given_name']), 'familyName' => self::text($row['family_name']),
            'bookings' => self::integer($row['bookings']), 'attended' => self::integer($row['attended']),
            'lateCancellations' => self::integer($row['late_cancellations']),
        ], $rows);
    }

    /** @return list<array{eventId:string,title:string,registered:int,revenue:int}> */
    public function events(DateTimeImmutable $start, DateTimeImmutable $end): array
    {
        $select = <<<'SQL'
            SELECT e.id, e.title, COUNT(CASE WHEN r.status = 'registered' THEN 1 END) AS registered,
                COALESCE(SUM(fee.gross), 0) + COALESCE(SUM(refund.gross), 0) AS revenue
            FROM events e
            LEFT JOIN event_registrations r ON r.event_id = e.id
            LEFT JOIN receipts fee ON fee.id = r.receipt_id
            LEFT JOIN receipts refund ON refund.id = r.cancellation_receipt_id
            WHERE EXISTS (SELECT 1 FROM lessons l WHERE l.event_id = e.id AND
            SQL;
        $rows = $this->db->fetchAllAssociative(
            $select . ' ' . self::RANGE . ') GROUP BY e.id, e.title ORDER BY e.id',
            $this->bounds($start, $end),
        );

        return array_map(fn(array $row): array => [
            'eventId' => $this->id($row['id']), 'title' => self::text($row['title']),
            'registered' => self::integer($row['registered']), 'revenue' => self::integer($row['revenue']),
        ], $rows);
    }

    /** @return list<SalesDay> */
    public function sales(Date $from, Date $to, string $currency): array
    {
        $sql = <<<'SQL'
            SELECT r.issue_date,
                SUM(CASE WHEN reg.id IS NULL AND r.kind = 'sale' THEN 1 WHEN reg.id IS NULL AND r.kind = 'cancellation' THEN -1 ELSE 0 END) AS passes_sold,
                SUM(CASE WHEN reg.id IS NULL THEN r.gross ELSE 0 END) AS sales_gross,
                SUM(CASE WHEN reg.id IS NOT NULL THEN r.gross ELSE 0 END) AS event_fees
            FROM receipts r
            LEFT JOIN event_registrations reg ON reg.receipt_id = COALESCE(r.cancels_receipt_id, r.id)
            WHERE r.currency = ? AND r.issue_date BETWEEN ? AND ? AND r.year BETWEEN ? AND ?
            GROUP BY r.issue_date
            SQL;
        $rows = $this->db->fetchAllAssociative(
            $sql,
            [$currency, (string) $from, (string) $to, (int) substr((string) $from, 0, 4), (int) substr((string) $to, 0, 4)],
        );

        return array_map(static fn(array $row): array => [
            'date' => self::text($row['issue_date']), 'passesSold' => self::integer($row['passes_sold']),
            'salesGross' => self::integer($row['sales_gross']), 'eventFees' => self::integer($row['event_fees']),
        ], $rows);
    }

    /** @return list<string> */
    private function bounds(DateTimeImmutable $start, DateTimeImmutable $end): array
    {
        return Database::sqlRange($this->db, $start, $end);
    }

    private function id(mixed $value): string
    {
        $id = self::text($value);

        return $this->isPostgres() ? $id : Uuid::fromBinary($id)->toRfc4122();
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private static function text(mixed $value): string
    {
        return is_string($value) ? $value : throw new RuntimeException('Expected a statistics text column');
    }

    private static function integer(mixed $value): int
    {
        return is_numeric($value) ? (int) $value : throw new RuntimeException('Expected a statistics integer column');
    }
}
