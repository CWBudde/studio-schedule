<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use Doctrine\DBAL\ArrayParameterType;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Service\MyLessonsPersistence;
use Symfony\Component\Uid\Uuid;

/** @phpstan-import-type FamilyBooking from MyLessonsPersistence */
final readonly class MyLessonsStore implements MyLessonsPersistence
{
    private const string BOOKING_TABLES = 'FROM bookings b JOIN lessons l ON l.id = b.lesson_id';

    public function __construct(private Connection $db) {}

    public function redemptionOnBooking(): bool
    {
        return (new LessonWriteStore($this->db))->redemptionOnBooking();
    }

    public function upcoming(array $people, DateTimeImmutable $now): array
    {
        return $this->bookings($people, "b.status <> 'canceled' AND l.ends_at > ?", [$now], 'ASC');
    }

    public function history(array $people, DateTimeImmutable $now, DateTimeImmutable $from, DateTimeImmutable $before): array
    {
        return $this->bookings($people, 'l.ends_at <= ? AND l.starts_at >= ? AND l.starts_at < ?', [$now, $from, $before], 'DESC');
    }

    public function hasHistoryBefore(array $people, DateTimeImmutable $now, DateTimeImmutable $before): bool
    {
        $ids = array_map($this->encodeId(...), $people);

        return $this->db->fetchOne(
            'SELECT 1 ' . self::BOOKING_TABLES . ' WHERE b.person_id IN (?) AND l.ends_at <= ? AND l.starts_at < ? LIMIT 1',
            [$ids, Database::sqlInstant($this->db, $now), Database::sqlInstant($this->db, $before)],
            [$this->idsType(), ParameterType::STRING, ParameterType::STRING],
        ) !== false;
    }

    public function waitingLessonIds(array $people, DateTimeImmutable $now): array
    {
        $ids = array_map($this->encodeId(...), $people);
        /** @var list<string> $ids */
        $ids = $this->db->fetchFirstColumn(
            'SELECT DISTINCT b.lesson_id FROM waitlist_entries b JOIN lessons l ON l.id = b.lesson_id WHERE '
            . "b.person_id IN (?) AND b.status IN ('waiting', 'invited') AND l.starts_at > ?",
            [$ids, Database::sqlInstant($this->db, $now)],
            [$this->idsType(), ParameterType::STRING],
        );

        return array_map($this->id(...), $ids);
    }

    /**
     * @param list<string> $people
     * @param list<DateTimeImmutable> $dates
     * @param 'ASC'|'DESC' $order
     * @return list<FamilyBooking>
     */
    private function bookings(array $people, string $filter, array $dates, string $order): array
    {
        $ids = array_map($this->encodeId(...), $people);
        /** @var list<array{id:string,lesson_id:string,person_id:string,status:string,source:string,trial:bool|int|string,late:bool|int|string,
         *   booked_at:string,canceled_at:?string,pass_id:?string,pass_name:string,pass_kind:string,visits_remaining:int|numeric-string|null,valid_until:?string}> $rows */
        $rows = $this->db->fetchAllAssociative(
            'SELECT b.id, b.lesson_id, b.person_id, b.status, b.source, b.trial, b.late, b.booked_at, b.canceled_at, '
            . 'p.id AS pass_id, pt.name AS pass_name, p.kind AS pass_kind, p.visits_remaining, p.valid_until '
            . self::BOOKING_TABLES . ' LEFT JOIN pass_redemptions r ON r.booking_id = b.id '
            . 'LEFT JOIN passes p ON p.id = r.pass_id LEFT JOIN pass_types pt ON pt.id = p.pass_type_id '
            . 'WHERE b.person_id IN (?)'
            . " AND $filter ORDER BY l.starts_at $order, l.id $order, b.booked_at $order, b.id $order",
            [$ids, ...array_map(fn(DateTimeImmutable $date): string => Database::sqlInstant($this->db, $date), $dates)],
            [$this->idsType(), ...array_fill(0, count($dates), ParameterType::STRING)],
        );

        return array_map(function (array $row): array {
            $pass = $row['pass_id'] === null ? null : [
                'id' => $this->id($row['pass_id']), 'name' => $row['pass_name'], 'kind' => $row['pass_kind'],
                'visitsRemaining' => (int) $row['visits_remaining'],
            ];
            if ($pass !== null && $row['valid_until'] !== null) {
                $pass['validUntil'] = $row['valid_until'];
            }

            return [
                'id' => $this->id($row['id']), 'lessonId' => $this->id($row['lesson_id']), 'personId' => $this->id($row['person_id']),
                'status' => $row['status'], 'source' => $row['source'], 'trial' => self::boolean($row['trial']),
                'late' => self::boolean($row['late']), 'bookedAt' => Instant::format(Database::instant($row['booked_at'])),
                'canceledAt' => $row['canceled_at'] === null ? null : Instant::format(Database::instant($row['canceled_at'])),
                'pass' => $pass,
            ];
        }, $rows);
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private function encodeId(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    private function id(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromBinary($id)->toRfc4122();
    }

    private function idsType(): ArrayParameterType
    {
        return $this->isPostgres() ? ArrayParameterType::STRING : ArrayParameterType::BINARY;
    }

    private static function boolean(bool|int|string $value): bool
    {
        return in_array($value, [true, 1, '1', 't'], true);
    }
}
