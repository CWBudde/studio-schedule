<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Domain\PublicEvent;
use StudioSchedule\Service\EventPersistence;
use Symfony\Component\Uid\Uuid;

final readonly class EventStore implements EventPersistence
{
    private const string EVENT = <<<'SQL'
        SELECT e.id, e.slug, e.title, e.description, e.course_type_id, ct.name AS course_type_name,
               e.capacity, e.price, e.vat_rate_bp, e.status, e.signup_opens_at, e.signup_closes_at, e.cancel_until,
               EXISTS (SELECT 1 FROM event_images i WHERE i.event_id = e.id) AS has_image
        FROM events e
        JOIN course_types ct ON ct.id = e.course_type_id
        SQL;

    private const string UPCOMING = <<<'SQL'
        WHERE e.status = 'published' AND EXISTS (
            SELECT 1 FROM lessons l WHERE l.event_id = e.id AND l.status = 'scheduled' AND l.ends_at > ?
        )
        ORDER BY (SELECT MIN(l.starts_at) FROM lessons l WHERE l.event_id = e.id), e.id
        SQL;

    private const string PUBLIC_SLUG = " WHERE e.slug = ? AND e.status IN ('published', 'canceled')";

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
        $rows = $this->connection->fetchAllAssociative(self::EVENT . "\n" . self::UPCOMING, [$at]);

        return array_map(static fn(array $row): PublicEvent => self::event($row, $postgres), $rows);
    }

    public function bySlug(string $slug): ?PublicEvent
    {
        $row = $this->connection->fetchAssociative(self::EVENT . self::PUBLIC_SLUG, [$slug]);

        return $row === false ? null : self::event($row, $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform);
    }

    public function image(string $slug): ?array
    {
        /** @var array{?string,string|resource|null}|false $row */
        $row = $this->connection->fetchNumeric(
            'SELECT i.image_type, i.image FROM events e LEFT JOIN event_images i ON i.event_id = e.id' . self::PUBLIC_SLUG,
            [$slug],
        );
        if ($row === false) {
            return null;
        }

        [$contentType, $data] = $row;
        if ($data !== null && !is_string($data)) {
            $stream = $data;
            $data = stream_get_contents($stream);
            fclose($stream);
            if ($data === false) {
                throw new RuntimeException('Could not read event image');
            }
        }

        return ['contentType' => $contentType, 'data' => $data];
    }

    /** @param array<string,mixed> $row */
    private static function event(array $row, bool $postgres): PublicEvent
    {
        return new PublicEvent(
            id: self::id($row['id'], $postgres),
            slug: self::text($row['slug']),
            title: self::text($row['title']),
            description: $row['description'] === null ? null : self::text($row['description']),
            courseType: ['id' => self::id($row['course_type_id'], $postgres), 'name' => self::text($row['course_type_name'])],
            capacity: self::integer($row['capacity']),
            price: self::integer($row['price']),
            vatRate: self::integer($row['vat_rate_bp']),
            status: self::text($row['status']),
            signupOpensAt: self::instant($row['signup_opens_at']),
            signupClosesAt: self::instant($row['signup_closes_at']),
            cancelUntil: self::instant($row['cancel_until']),
            hasImage: in_array($row['has_image'], [true, 1, '1', 't'], true),
        );
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
