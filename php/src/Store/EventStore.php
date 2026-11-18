<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Domain\PublishedEvent;
use StudioSchedule\Service\EventPersistence;
use Symfony\Component\Uid\Uuid;

final readonly class EventStore implements EventPersistence
{
    private const string UPCOMING = <<<'SQL'
        SELECT e.id, e.slug, e.title, e.description, e.course_type_id, ct.name AS course_type_name,
               e.capacity, e.price, e.vat_rate_bp, e.signup_opens_at, e.signup_closes_at, e.cancel_until,
               EXISTS (SELECT 1 FROM event_images i WHERE i.event_id = e.id) AS has_image
        FROM events e
        JOIN course_types ct ON ct.id = e.course_type_id
        WHERE e.status = 'published' AND EXISTS (
            SELECT 1 FROM lessons l WHERE l.event_id = e.id AND l.status = 'scheduled' AND l.ends_at > ?
        )
        ORDER BY (SELECT MIN(l.starts_at) FROM lessons l WHERE l.event_id = e.id), e.id
        SQL;

    private ScheduleStore $schedule;

    public function __construct(private Connection $connection)
    {
        $this->schedule = new ScheduleStore($connection);
    }

    public function inReadSnapshot(Closure $read): mixed
    {
        return Database::inReadSnapshot($this->connection, $read);
    }

    public function upcoming(DateTimeImmutable $now): array
    {
        $postgres = $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $at = $now->setTimezone(new DateTimeZone('UTC'))->format($postgres ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u');
        $rows = $this->connection->fetchAllAssociative(self::UPCOMING, [$at]);

        return array_map(static fn(array $row): PublishedEvent => new PublishedEvent(
            id: self::id($row['id'], $postgres),
            slug: self::text($row['slug']),
            title: self::text($row['title']),
            description: $row['description'] === null ? null : self::text($row['description']),
            courseType: ['id' => self::id($row['course_type_id'], $postgres), 'name' => self::text($row['course_type_name'])],
            capacity: self::integer($row['capacity']),
            price: self::integer($row['price']),
            vatRate: self::integer($row['vat_rate_bp']),
            signupOpensAt: self::instant($row['signup_opens_at']),
            signupClosesAt: self::instant($row['signup_closes_at']),
            cancelUntil: self::instant($row['cancel_until']),
            hasImage: in_array($row['has_image'], [true, 1, '1', 't'], true),
        ), $rows);
    }

    public function sessions(array $eventIds): array
    {
        return $this->schedule->eventSessions($eventIds);
    }

    public function occupiedPlaces(array $eventIds, DateTimeImmutable $now): array
    {
        return $this->schedule->occupiedEventPlaces($eventIds, $now);
    }

    private static function id(mixed $value, bool $postgres): string
    {
        $text = self::text($value);

        return $postgres ? $text : Uuid::fromBinary($text)->toRfc4122();
    }

    private static function text(mixed $value): string
    {
        return is_string($value) ? $value : throw new RuntimeException('Expected an event text column');
    }

    private static function integer(mixed $value): int
    {
        return is_numeric($value) ? (int) $value : throw new RuntimeException('Expected an event integer column');
    }

    private static function instant(mixed $value): ?DateTimeImmutable
    {
        return $value === null ? null : new DateTimeImmutable(self::text($value), new DateTimeZone('UTC'));
    }
}
