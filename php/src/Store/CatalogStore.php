<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\ForeignKeyConstraintViolationException;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Domain\CatalogResource;
use StudioSchedule\Service\CatalogAdminPersistence;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\CatalogPersistence;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type CatalogData from CatalogAdminPersistence
 * @phpstan-import-type CatalogDocument from CatalogAdminPersistence
 */
final readonly class CatalogStore implements CatalogPersistence, CatalogAdminPersistence
{
    public function __construct(private Connection $connection) {}

    public function locations(): array
    {
        $items = [];
        foreach ($this->connection->fetchAllNumeric('SELECT id, name, address FROM locations') as [$id, $name, $address]) {
            $items[] = [
                'id' => $this->id($id), 'name' => self::text($name),
                'address' => $address === null ? null : self::text($address),
            ];
        }

        return $items;
    }

    public function rooms(?string $location): ?array
    {
        $columns = 'SELECT r.id, r.location_id, r.name, r.capacity, r.position, l.name';
        if ($location === null) {
            $rows = $this->connection->fetchAllNumeric($columns . ' FROM rooms r JOIN locations l ON l.id = r.location_id');
        } else {
            [$value, $type] = $this->isPostgres()
                ? [$location, ParameterType::STRING]
                : [Uuid::fromString($location)->toBinary(), ParameterType::BINARY];
            // A surviving row with NULL room columns distinguishes an empty location from a missing one.
            $rows = $this->connection->fetchAllNumeric(
                $columns . ' FROM locations l LEFT JOIN rooms r ON r.location_id = l.id WHERE l.id = ?',
                [$value],
                [$type],
            );
            if ($rows === []) {
                return null;
            }
        }

        $items = [];
        foreach ($rows as [$id, $locationId, $name, $capacity, $position, $locationName]) {
            if ($id === null) {
                continue;
            }
            $items[] = [
                'id' => $this->id($id), 'locationId' => $this->id($locationId), 'name' => self::text($name),
                'capacity' => self::number($capacity), 'position' => self::number($position), 'locationName' => self::text($locationName),
            ];
        }

        return $items;
    }

    public function staff(): array
    {
        $items = [];
        foreach ($this->connection->fetchAllNumeric('SELECT id, display_name, kind FROM staff WHERE active = TRUE') as [$id, $name, $kind]) {
            $items[] = ['id' => $this->id($id), 'displayName' => self::text($name), 'kind' => self::text($kind)];
        }

        return $items;
    }

    public function courseTypes(): array
    {
        $items = [];
        foreach ($this->connection->fetchAllNumeric('SELECT id, name, description FROM course_types') as [$id, $name, $description]) {
            $items[] = [
                'id' => $this->id($id), 'name' => self::text($name),
                'description' => $description === null ? null : self::text($description),
            ];
        }

        return $items;
    }

    public function levels(): array
    {
        $items = [];
        foreach ($this->connection->fetchAllNumeric('SELECT id, name, code, position FROM levels') as [$id, $name, $code, $position]) {
            $items[] = [
                'id' => $this->id($id), 'name' => self::text($name),
                'code' => $code === null ? null : self::text($code), 'position' => self::number($position),
            ];
        }

        return $items;
    }

    public function terms(): array
    {
        // Read dates under the ISO DateStyle set by Database::inReadSnapshot.
        $rows = Database::inReadSnapshot($this->connection, fn(): array => $this->connection->fetchAllNumeric(
            'SELECT id, name, starts_on, ends_on FROM terms',
        ));
        $items = [];
        foreach ($rows as [$id, $name, $startsOn, $endsOn]) {
            $items[] = [
                'id' => $this->id($id), 'name' => self::text($name),
                'startsOn' => self::text($startsOn), 'endsOn' => self::text($endsOn),
            ];
        }

        return $items;
    }

    public function allStaff(): array
    {
        $items = [];
        foreach ($this->connection->fetchAllAssociative(
            'SELECT s.id, s.display_name, s.kind, s.active, s.person_id, p.given_name, p.family_name, p.email, '
            . 'p.member_no, p.anonymized_at FROM staff s LEFT JOIN persons p ON p.id = s.person_id',
        ) as $row) {
            $entry = ['id' => $this->id($row['id']), 'displayName' => self::text($row['display_name']),
                'kind' => self::text($row['kind']), 'active' => in_array($row['active'], [true, 1, '1', 't'], true)];
            if ($row['person_id'] !== null) {
                $entry['person'] = $this->person($row);
            }
            $items[] = $entry;
        }
        usort($items, static fn(array $a, array $b): int => strcmp($a['displayName'], $b['displayName']) ?: strcmp($a['id'], $b['id']));

        return $items;
    }

    public function save(CatalogResource $resource, ?string $id, Closure $data): array
    {
        $write = function () use ($resource, $id, $data): array {
            $table = self::table($resource);
            $key = $id ?? Uuid::v7()->toRfc4122();
            $storedId = $this->encodeId($key);
            // A staff link takes the person lock before locking the staff row, as in Go.
            $previous = $id === null ? null : $this->row($table, $storedId, lock: $resource !== CatalogResource::Staff);
            $record = $data();
            [$fields, $document] = $this->columns($resource, $record, $previous);
            $types = $resource === CatalogResource::Staff ? ['active' => ParameterType::BOOLEAN] : [];
            if ($id === null) {
                $this->connection->insert($table, ['id' => $storedId] + $fields, $types);
            } else {
                if ($resource === CatalogResource::Staff) {
                    $this->row($table, $storedId);
                }
                $this->connection->update($table, $fields, ['id' => $storedId], $types);
            }

            return ['id' => $key] + $document;
        };

        try {
            return $resource === CatalogResource::Level && $id === null
                ? $this->writeLevelScale($write)
                : Database::retryWrite($this->connection, $write);
        } catch (UniqueConstraintViolationException) {
            throw new CatalogFailure(409, 'a record with these values exists already', reason: 'duplicate');
        } catch (ForeignKeyConstraintViolationException $failure) {
            throw match ($resource) {
                CatalogResource::Room => CatalogFailure::field('locationId', 'location does not exist'),
                CatalogResource::Staff => CatalogFailure::field('personId', 'person does not exist'),
                default => $failure,
            };
        }
    }

    public function delete(CatalogResource $resource, string $id): void
    {
        try {
            Database::retryWrite($this->connection, function () use ($resource, $id): void {
                if ($this->connection->delete(self::table($resource), ['id' => $this->encodeId($id)]) === 0) {
                    throw new CatalogFailure(404, 'resource not found');
                }
            });
        } catch (ForeignKeyConstraintViolationException) {
            throw new CatalogFailure(409, 'the record is still in use', reason: 'in-use');
        }
    }

    public function orderRooms(string $location, array $ids): void
    {
        Database::retryWrite($this->connection, function () use ($location, $ids): void {
            $stored = $this->encodeId($location);
            $this->row('locations', $stored);
            /** @var list<string> $current */
            $current = $this->connection->fetchFirstColumn('SELECT id FROM rooms WHERE location_id = ? ORDER BY id FOR UPDATE', [$stored]);
            $this->setOrder('rooms', 'roomIds', $current, $ids);
        });
    }

    public function orderLevels(array $ids): void
    {
        $this->writeLevelScale(function () use ($ids): void {
            /** @var list<string> $current */
            $current = $this->connection->fetchFirstColumn('SELECT id FROM levels ORDER BY id FOR UPDATE');
            $this->setOrder('levels', 'levelIds', $current, $ids);
        });
    }

    /**
     * @param CatalogData $record
     * @param array<string,mixed>|null $previous
     * @return array{CatalogData, CatalogDocument}
     */
    private function columns(CatalogResource $resource, array $record, ?array $previous): array
    {
        switch ($resource) {
            case CatalogResource::Room:
                $location = $this->encodeId(self::text($record['locationId']));
                if ($previous === null || $previous['location_id'] !== $location) {
                    if ($this->connection->fetchOne('SELECT id FROM locations WHERE id = ? FOR UPDATE', [$location]) === false) {
                        throw CatalogFailure::field('locationId', 'location does not exist');
                    }
                    $position = self::number($this->connection->fetchOne('SELECT COALESCE(MAX(position), 0) + 1 FROM rooms WHERE location_id = ?', [$location]));
                } else {
                    $position = self::number($previous['position']);
                }

                return [['location_id' => $location, 'name' => $record['name'], 'capacity' => $record['capacity'], 'position' => $position],
                    self::present($record + ['position' => $position])];

            case CatalogResource::Level:
                $position = self::number($previous === null
                    ? $this->connection->fetchOne('SELECT COALESCE(MAX(position), 0) + 1 FROM levels')
                    : $previous['position']);
                $fields = $previous === null ? $record + ['position' => $position] : $record;

                return [$fields, self::present($record + ['position' => $position])];

            case CatalogResource::Staff:
                return $this->staffColumns($record);

            default:
                return [$record, self::present($record)];
        }
    }

    /**
     * @param CatalogData $record
     * @return array{CatalogData, CatalogDocument}
     */
    private function staffColumns(array $record): array
    {
        $personId = $record['personId'];
        unset($record['personId']);
        $fields = ['display_name' => $record['displayName'], 'kind' => $record['kind'], 'active' => $record['active'], 'person_id' => null];
        $document = self::present($record);
        if ($personId !== null) {
            $fields['person_id'] = $this->encodeId(self::text($personId));
            $person = $this->connection->fetchAssociative(
                'SELECT id AS person_id, given_name, family_name, email, member_no, anonymized_at FROM persons '
                . 'WHERE id = ? AND merged_into IS NULL AND anonymized_at IS NULL FOR UPDATE',
                [$fields['person_id']],
            );
            if ($person === false) {
                throw CatalogFailure::field('personId', 'person does not exist');
            }
            $document['person'] = $this->person($person);
        }

        return [$fields, $document];
    }

    /**
     * @param array<string,mixed> $row
     * @return array<string,string|bool>
     */
    private function person(array $row): array
    {
        $person = ['id' => $this->id($row['person_id']), 'givenName' => self::text($row['given_name']), 'familyName' => self::text($row['family_name'])];
        foreach (['email' => 'email', 'memberNo' => 'member_no'] as $field => $column) {
            if ($row[$column] !== null && $row[$column] !== '') {
                $person[$field] = self::text($row[$column]);
            }
        }
        if ($row['anonymized_at'] !== null) {
            $person['anonymized'] = true;
        }

        return $person;
    }

    /**
     * @param list<string> $current
     * @param list<string> $requested
     */
    private function setOrder(string $table, string $field, array $current, array $requested): void
    {
        $encoded = array_map($this->encodeId(...), $requested);
        $comparison = $encoded;
        sort($comparison, SORT_STRING);
        sort($current, SORT_STRING);
        if ($comparison !== $current) {
            throw CatalogFailure::field($field, 'must list each record exactly once');
        }
        foreach ($encoded as $offset => $id) {
            $this->connection->update($table, ['position' => $offset + 1], ['id' => $id]);
        }
    }

    /**
     * @template T
     * @param Closure():T $write
     * @return T
     */
    private function writeLevelScale(Closure $write): mixed
    {
        if ($this->isPostgres()) {
            return Database::retryWrite($this->connection, function () use ($write): mixed {
                $this->connection->fetchOne("SELECT pg_advisory_xact_lock(hashtext('level-scale'))");

                return $write();
            });
        }
        $name = "SHA2(CONCAT('studio-schedule:', DATABASE(), ':level-scale'), 256)";
        $held = false;
        try {
            return Database::retryWrite($this->connection, function () use ($name, &$held, $write): mixed {
                // Retry may reconnect. Only acquire the named lock if this session does not already hold it.
                $locked = $this->connection->fetchOne(
                    "SELECT IF(IS_USED_LOCK($name) = CONNECTION_ID(), 1, GET_LOCK($name, @@SESSION.innodb_lock_wait_timeout))",
                );
                if (!in_array($locked, [true, 1, '1'], true)) {
                    throw new RuntimeException('Could not lock the level scale');
                }
                $held = true;

                return $write();
            });
        } finally {
            if ($held) {
                $this->connection->fetchOne("SELECT RELEASE_LOCK($name)");
            }
        }
    }

    /** @return array<string,mixed> */
    private function row(string $table, string $id, bool $lock = true): array
    {
        $row = $this->connection->fetchAssociative("SELECT * FROM $table WHERE id = ?" . ($lock ? ' FOR UPDATE' : ''), [$id]);

        return $row === false ? throw new CatalogFailure(404, 'resource not found') : $row;
    }

    private static function table(CatalogResource $resource): string
    {
        return $resource === CatalogResource::CourseType ? 'course_types' : $resource->value;
    }

    /**
     * @param CatalogData $record
     * @return array<string,string|int|bool>
     */
    private static function present(array $record): array
    {
        return array_filter($record, static fn(string|int|bool|null $value): bool => $value !== null && $value !== '');
    }

    private function encodeId(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private function id(mixed $value): string
    {
        $id = self::text($value);

        return $this->isPostgres() ? $id : Uuid::fromBinary($id)->toRfc4122();
    }

    private static function text(mixed $value): string
    {
        return is_string($value) ? $value : throw new RuntimeException('Expected a catalogue text column');
    }

    private static function number(mixed $value): int
    {
        return is_numeric($value) ? (int) $value : throw new RuntimeException('Expected a catalogue integer column');
    }
}
