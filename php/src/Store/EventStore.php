<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\ArrayParameterType;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\ForeignKeyConstraintViolationException;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\Event;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\EventData;
use StudioSchedule\Service\EventPersistence;
use Symfony\Component\Uid\Uuid;

final readonly class EventStore implements EventPersistence
{
    private const string EVENT = <<<'SQL'
        SELECT e.id, e.slug, e.title, e.description, e.course_type_id, ct.name AS course_type_name,
               e.capacity, e.price, e.vat_rate_bp, e.status, e.signup_opens_at, e.signup_closes_at, e.cancel_until,
               e.created_at, e.updated_at, EXISTS (SELECT 1 FROM event_images i WHERE i.event_id = e.id) AS has_image
        FROM events e
        JOIN course_types ct ON ct.id = e.course_type_id
        SQL;

    private const string UPCOMING = <<<'SQL'
        WHERE e.status = 'published' AND EXISTS (
            SELECT 1 FROM lessons l WHERE l.event_id = e.id AND l.status = 'scheduled' AND l.ends_at > ?
        )
        ORDER BY (SELECT MIN(l.starts_at) FROM lessons l WHERE l.event_id = e.id), e.id
        SQL;

    private const string OFFICE_ORDER = <<<'SQL'
        LEFT JOIN (
            SELECT event_id, MIN(starts_at) AS starts_at FROM lessons WHERE event_id IS NOT NULL GROUP BY event_id
        ) first_session ON first_session.event_id = e.id
        ORDER BY CASE WHEN first_session.starts_at IS NULL THEN 1 ELSE 0 END, first_session.starts_at, e.id
        SQL;

    private const string IMAGE = 'SELECT i.image_type, i.image FROM events e LEFT JOIN event_images i ON i.event_id = e.id';
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

    public function write(Closure $operation): mixed
    {
        return Database::retryWrite($this->connection, function () use ($operation): mixed {
            if ($this->isPostgres()) {
                $this->connection->executeStatement("SET LOCAL DateStyle TO 'ISO, YMD'");
            }

            return $operation();
        }, readCommitted: true);
    }

    public function slugInUse(string $slug): bool
    {
        return $this->connection->fetchOne('SELECT 1 FROM events WHERE slug = ?', [$slug]) !== false;
    }

    public function createDraft(EventData $data, string $slug, string $actor, DateTimeImmutable $now): Event
    {
        $course = $this->encodeId($data->courseTypeId);
        if ($this->connection->fetchOne('SELECT id FROM course_types WHERE id = ? FOR UPDATE', [$course], [$this->idType()]) === false) {
            throw CatalogFailure::field('courseTypeId', 'course type does not exist');
        }

        $id = Uuid::v7()->toRfc4122();
        $instant = Database::sqlInstant($this->connection, $now);
        try {
            $this->connection->insert('events', [
                'id' => $this->encodeId($id), 'slug' => $slug, 'title' => $data->title, 'description' => $data->description,
                'course_type_id' => $course, 'capacity' => $data->capacity, 'price' => $data->price, 'vat_rate_bp' => $data->vatRate,
                'status' => 'draft', 'signup_opens_at' => $this->sqlInstant($data->signupOpensAt),
                'signup_closes_at' => $this->sqlInstant($data->signupClosesAt), 'cancel_until' => $this->sqlInstant($data->cancelUntil),
                'created_at' => $instant, 'updated_at' => $instant, 'created_by' => $this->encodeId($actor),
            ]);
        } catch (UniqueConstraintViolationException) {
            throw new CatalogFailure(409, 'a record with these values exists already', reason: 'duplicate');
        }

        return $this->officeEvent($id) ?? throw new RuntimeException('The new event was not stored');
    }

    public function lockEvent(string $id): ?Event
    {
        $found = $this->connection->fetchOne(
            'SELECT id FROM events WHERE id = ? FOR UPDATE',
            [$this->encodeId($id)],
            [$this->idType()],
        );

        return $found === false ? null : $this->officeEvent($id);
    }

    public function hasRegistrations(string $id, bool $onlyOpen): bool
    {
        $status = $onlyOpen ? " AND status IN ('waiting', 'invited', 'registered')" : '';

        return $this->connection->fetchOne(
            'SELECT 1 FROM event_registrations WHERE event_id = ?' . $status . ' LIMIT 1',
            [$this->encodeId($id)],
            [$this->idType()],
        ) !== false;
    }

    public function deleteEvent(string $id, array $sessions): void
    {
        $type = $this->idType();
        // The caller holds the event lock. Take session locks in their start order.
        try {
            foreach ($sessions as $session) {
                $lesson = $this->encodeId($session->id);
                $this->connection->fetchOne('SELECT id FROM lessons WHERE id = ? FOR UPDATE', [$lesson], [$type]);
                $this->connection->executeStatement('DELETE FROM lessons WHERE id = ?', [$lesson], [$type]);
            }
            $this->connection->executeStatement('DELETE FROM events WHERE id = ?', [$this->encodeId($id)], [$type]);
        } catch (ForeignKeyConstraintViolationException) {
            throw new CatalogFailure(409, 'conflict');
        }
    }

    public function setStatus(string $id, string $status, DateTimeImmutable $now): Event
    {
        $this->connection->executeStatement(
            'UPDATE events SET status = ?, updated_at = ? WHERE id = ?',
            [$status, Database::sqlInstant($this->connection, $now), $this->encodeId($id)],
            [ParameterType::STRING, ParameterType::STRING, $this->idType()],
        );

        return $this->officeEvent($id) ?? throw new RuntimeException('The event disappeared during its update');
    }

    public function setImage(string $id, string $contentType, #[SensitiveParameter] string $data, string $digest): void
    {
        $update = $this->isPostgres()
            ? 'ON CONFLICT (event_id) DO UPDATE SET image = EXCLUDED.image, image_type = EXCLUDED.image_type, image_digest = EXCLUDED.image_digest'
            : 'ON DUPLICATE KEY UPDATE image = VALUES(image), image_type = VALUES(image_type), image_digest = VALUES(image_digest)';
        $this->connection->executeStatement(
            "INSERT INTO event_images (event_id, image, image_type, image_digest) VALUES (?, ?, ?, ?) $update",
            [$this->encodeId($id), $data, $contentType, $digest],
            [$this->idType(), ParameterType::BINARY, ParameterType::STRING, ParameterType::STRING],
        );
    }

    public function removeImage(string $id): void
    {
        $this->connection->executeStatement('DELETE FROM event_images WHERE event_id = ?', [$this->encodeId($id)], [$this->idType()]);
    }

    private function sqlInstant(?DateTimeImmutable $value): ?string
    {
        return $value === null ? null : Database::sqlInstant($this->connection, $value);
    }

    public function upcoming(DateTimeImmutable $now): array
    {
        $postgres = $this->isPostgres();
        $at = Database::sqlInstant($this->connection, $now);
        $rows = $this->connection->fetchAllAssociative(self::EVENT . "\n" . self::UPCOMING, [$at]);

        return array_map(static fn(array $row): Event => self::event($row, $postgres), $rows);
    }

    public function bySlug(string $slug): ?Event
    {
        $row = $this->connection->fetchAssociative(self::EVENT . self::PUBLIC_SLUG, [$slug]);

        return $row === false ? null : self::event($row, $this->isPostgres());
    }

    public function officeList(): array
    {
        $postgres = $this->isPostgres();
        $rows = $this->connection->fetchAllAssociative(self::EVENT . "\n" . self::OFFICE_ORDER);

        return array_map(static fn(array $row): Event => self::event($row, $postgres), $rows);
    }

    public function officeEvent(string $id): ?Event
    {
        $row = $this->connection->fetchAssociative(
            self::EVENT . ' WHERE e.id = ?',
            [$this->encodeId($id)],
            [$this->idType()],
        );

        return $row === false ? null : self::event($row, $this->isPostgres());
    }

    public function imageInfo(string $id): ?array
    {
        $row = $this->connection->fetchNumeric(
            'SELECT image_type, image_digest FROM event_images WHERE event_id = ?',
            [$this->encodeId($id)],
            [$this->idType()],
        );

        return $row === false ? null : ['contentType' => self::text($row[0]), 'digest' => self::text($row[1])];
    }

    public function image(string $slug): ?array
    {
        return $this->readImage(self::PUBLIC_SLUG, $slug, ParameterType::STRING);
    }

    public function imageById(string $id): ?array
    {
        return $this->readImage(' WHERE e.id = ?', $this->encodeId($id), $this->idType());
    }

    /**
     * @param ' WHERE e.id = ?'|self::PUBLIC_SLUG $where
     * @return array{contentType:?string,data:?string}|null
     */
    private function readImage(string $where, string $key, ParameterType $type): ?array
    {
        /** @var array{?string,string|resource|null}|false $row */
        $row = $this->connection->fetchNumeric(self::IMAGE . $where, [$key], [$type]);
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
    private static function event(array $row, bool $postgres): Event
    {
        return new Event(
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
            createdAt: Database::instant(self::text($row['created_at'])),
            updatedAt: Database::instant(self::text($row['updated_at'])),
        );
    }

    public function sessions(?array $eventIds): array
    {
        return $this->schedule->eventSessions($eventIds);
    }

    public function occupiedPlaces(array $eventIds, DateTimeImmutable $now): array
    {
        return $this->schedule->occupiedEventPlaces($eventIds, $now);
    }

    public function registrationCounts(?array $eventIds, DateTimeImmutable $now): array
    {
        $postgres = $this->isPostgres();
        $params = [Database::sqlInstant($this->connection, $now)];
        $types = [ParameterType::STRING];
        $onlyEvents = '';
        if ($eventIds !== null) {
            $onlyEvents = ' AND event_id IN (?)';
            $params[] = array_map($this->encodeId(...), $eventIds);
            $types[] = $postgres ? ArrayParameterType::STRING : ArrayParameterType::BINARY;
        }
        $rows = $this->connection->fetchAllAssociative(
            "SELECT event_id, COUNT(CASE WHEN status = 'registered' THEN 1 END) AS registered, "
            . "COUNT(CASE WHEN status = 'invited' AND expires_at > ? THEN 1 END) AS invited, "
            . "COUNT(CASE WHEN status = 'waiting' THEN 1 END) AS waiting FROM event_registrations "
            . "WHERE status IN ('registered', 'invited', 'waiting')$onlyEvents GROUP BY event_id",
            $params,
            $types,
        );
        $counts = [];
        foreach ($rows as $row) {
            $counts[self::id($row['event_id'], $postgres)] = [
                'registered' => self::integer($row['registered']),
                'invited' => self::integer($row['invited']),
                'waiting' => self::integer($row['waiting']),
            ];
        }

        return $counts;
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private function idType(): ParameterType
    {
        return $this->isPostgres() ? ParameterType::STRING : ParameterType::BINARY;
    }

    private function encodeId(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
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
        return $value === null ? null : Database::instant(self::text($value));
    }
}
