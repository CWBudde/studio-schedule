<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\ForeignKeyConstraintViolationException;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Domain\CatalogResource;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Domain\TimeOfDay;
use StudioSchedule\Service\CatalogAdminPersistence;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\CatalogPersistence;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type CatalogData from CatalogAdminPersistence
 * @phpstan-import-type CatalogDocument from CatalogAdminPersistence
 * @phpstan-import-type PassTypeDocument from CatalogAdminPersistence
 * @phpstan-import-type PersonMatch from CatalogPersistence
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

    public function classes(string $term, bool $withRetired): ?array
    {
        $active = $withRetired ? '' : ' AND c.retired = FALSE';
        // Keep an empty term distinguishable from an unknown term using a single read.
        $rows = $this->connection->fetchAllAssociative(
            $this->classQuery("terms t LEFT JOIN classes c ON c.term_id = t.id$active")
            . 'WHERE t.id = ? ORDER BY c.weekday, c.start_time, c.id',
            [$this->encodeId($term)],
        );
        if ($rows === []) {
            return null;
        }

        $items = [];
        foreach ($rows as $row) {
            if ($row['id'] !== null) {
                $items[] = $this->classDocument($row, $withRetired);
            }
        }

        return $items;
    }

    /** @return CatalogDocument */
    public function classByStoredId(string $id): array
    {
        $row = $this->connection->fetchAssociative($this->classQuery('classes c') . 'WHERE c.id = ?', [$id]);
        if ($row === false) {
            throw new CatalogFailure(404, 'resource not found');
        }

        return $this->classDocument($row, true);
    }

    private function classQuery(string $source): string
    {
        $duration = $this->isPostgres() ? Database::intervalMicros('c.duration') : 'c.duration';

        return "SELECT c.id, c.term_id, c.course_type_id, c.room_id, c.weekday, c.start_time, $duration AS duration_micros, "
            . 'c.capacity, c.retired, c.all_levels, c.stage, c.age_from_months, c.age_to_months, '
            . 'i.id AS instructor_id, i.display_name AS instructor_name, a.id AS accompanist_id, a.display_name AS accompanist_name, '
            . 'f.id AS from_id, f.name AS from_name, f.code AS from_code, f.position AS from_position, '
            . 'l.id AS to_id, l.name AS to_name, l.code AS to_code, l.position AS to_position '
            . "FROM $source "
            . 'LEFT JOIN staff i ON i.id = c.instructor_id LEFT JOIN staff a ON a.id = c.accompanist_id '
            . 'LEFT JOIN levels f ON f.id = c.level_from_id LEFT JOIN levels l ON l.id = c.level_to_id ';
    }

    public function classMembers(string $class): ?array
    {
        $rows = Database::inReadSnapshot($this->connection, fn(): array => $this->connection->fetchAllAssociative(
            'SELECT m.person_id, m.since, m.paused_until, p.given_name, p.family_name, p.email, p.member_no, p.anonymized_at, s.timezone '
            . 'FROM classes c LEFT JOIN class_members m ON m.class_id = c.id LEFT JOIN persons p ON p.id = m.person_id '
            . 'LEFT JOIN settings s ON s.id = TRUE WHERE c.id = ?',
            [$this->encodeId($class)],
        ));
        if ($rows === []) {
            return null;
        }

        $members = [];
        foreach ($rows as $row) {
            if ($row['person_id'] !== null) {
                $members[] = [
                    'person' => $this->person($row),
                    'since' => $row['since'] === null ? null : self::text($row['since']),
                    'pausedUntil' => $row['paused_until'] === null ? null : self::text($row['paused_until']),
                ];
            }
        }
        $zone = $rows[0]['timezone'];

        return ['timezone' => $zone === null ? null : self::text($zone), 'members' => $members];
    }

    /**
     * @param array<string,mixed> $row
     * @return CatalogDocument
     */
    private function classDocument(array $row, bool $withRetired): array
    {
        $entry = [
            'id' => $this->id($row['id']), 'termId' => $this->id($row['term_id']),
            'courseTypeId' => $this->id($row['course_type_id']), 'roomId' => $this->id($row['room_id']),
            'instructor' => ['id' => $this->id($row['instructor_id']), 'displayName' => self::text($row['instructor_name'])],
            'weekday' => self::number($row['weekday']), 'startTime' => (string) TimeOfDay::parse(substr(self::text($row['start_time']), 0, 5)),
            'durationMinutes' => intdiv(self::number($row['duration_micros']), 60_000_000), 'capacity' => self::number($row['capacity']),
        ];
        if ($row['accompanist_id'] !== null) {
            $entry['accompanist'] = ['id' => $this->id($row['accompanist_id']), 'displayName' => self::text($row['accompanist_name'])];
        }
        if ($row['from_id'] !== null) {
            $from = $this->classLevel($row, 'from');
            $to = $this->classLevel($row, 'to');
            if ([$row['from_position'], $from['id']] > [$row['to_position'], $to['id']]) {
                [$from, $to] = [$to, $from];
            }
            $entry['levelFrom'] = $from;
            $entry['levelTo'] = $to;
        }
        if (in_array($row['all_levels'], [true, 1, '1', 't'], true)) {
            $entry['allLevels'] = true;
        }
        foreach (['stage' => 'stage', 'ageFromMonths' => 'age_from_months', 'ageToMonths' => 'age_to_months'] as $key => $column) {
            if ($row[$column] !== null) {
                $entry[$key] = self::number($row[$column]);
            }
        }
        if ($withRetired) {
            $entry['retired'] = in_array($row['retired'], [true, 1, '1', 't'], true);
        }

        return $entry;
    }

    /**
     * @param array<string,mixed> $row
     * @return array{id: string, name: string, code?: string}
     */
    private function classLevel(array $row, string $end): array
    {
        $level = ['id' => $this->id($row[$end . '_id']), 'name' => self::text($row[$end . '_name'])];
        $code = $row[$end . '_code'];
        if ($code !== null && $code !== '') {
            $level['code'] = self::text($code);
        }

        return $level;
    }

    public function allStaff(): array
    {
        return $this->staffRecords();
    }

    /** @return list<CatalogDocument> */
    public function staffForPerson(string $personId): array
    {
        return $this->staffRecords($personId);
    }

    /** @return list<CatalogDocument> */
    private function staffRecords(?string $personId = null): array
    {
        $items = [];
        foreach ($this->connection->fetchAllAssociative(
            'SELECT s.id, s.display_name, s.kind, s.active, s.person_id, p.given_name, p.family_name, p.email, '
            . 'p.member_no, p.anonymized_at FROM staff s LEFT JOIN persons p ON p.id = s.person_id'
            . ($personId === null ? '' : ' WHERE s.person_id = ?'),
            $personId === null ? [] : [$this->encodeId($personId)],
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
        $this->deleteRecord(self::table($resource), $id);
    }

    private function deleteRecord(string $table, string $id): void
    {
        try {
            Database::retryWrite($this->connection, function () use ($table, $id): void {
                if ($this->connection->delete($table, ['id' => $this->encodeId($id)]) === 0) {
                    throw new CatalogFailure(404, 'resource not found');
                }
            });
        } catch (ForeignKeyConstraintViolationException) {
            throw new CatalogFailure(409, 'the record is still in use', reason: 'in-use');
        }
    }

    public function passTypes(): array
    {
        return Database::inReadSnapshot($this->connection, function (): array {
            $name = $this->isPostgres() ? 'name' : 'CAST(name AS BINARY)';
            $rows = $this->connection->fetchAllAssociative(
                "SELECT id, name, kind, visits, validity_days, price, vat_rate_bp, active, created_at FROM pass_types ORDER BY $name, id",
            );

            return array_map($this->passType(...), $rows);
        });
    }

    public function savePassType(?string $id, Closure $data, DateTimeImmutable $now): array
    {
        try {
            return $this->writePassType(function () use ($id, $data, $now): array {
                $stored = $this->encodeId($id ?? Uuid::v7()->toRfc4122());
                if ($id !== null) {
                    $this->row('pass_types', $stored);
                }
                $pass = $data();
                $fields = [
                    'name' => $pass['name'], 'kind' => $pass['kind'], 'visits' => $pass['visits'] ?: null,
                    'validity_days' => $pass['validityDays'], 'price' => $pass['price'], 'vat_rate_bp' => $pass['vatRate'],
                    'active' => $pass['active'],
                ];
                $types = ['active' => ParameterType::BOOLEAN, 'visits' => $fields['visits'] === null ? ParameterType::NULL : ParameterType::INTEGER];
                if ($id === null) {
                    $fields['created_at'] = Database::sqlInstant($this->connection, $now);
                    $this->connection->insert('pass_types', ['id' => $stored] + $fields, $types);
                } else {
                    $this->connection->update('pass_types', $fields, ['id' => $stored], $types);
                }

                return $this->passType($this->row('pass_types', $stored, lock: false));
            });
        } catch (UniqueConstraintViolationException) {
            throw new CatalogFailure(409, 'a record with these values exists already', reason: 'duplicate');
        }
    }

    public function retirePassType(string $id): array
    {
        return $this->writePassType(function () use ($id): array {
            $stored = $this->encodeId($id);
            $type = $this->passType($this->row('pass_types', $stored));
            if ($type['active']) {
                $this->connection->update('pass_types', ['active' => false], ['id' => $stored], ['active' => ParameterType::BOOLEAN]);
                $type['active'] = false;
            }

            return $type;
        });
    }

    /**
     * @param Closure():PassTypeDocument $write
     * @return PassTypeDocument
     */
    private function writePassType(Closure $write): array
    {
        return Database::retryWrite($this->connection, function () use ($write): array {
            if ($this->isPostgres()) {
                // Read the persisted creation time in the same format as a read snapshot.
                $this->connection->executeStatement("SET LOCAL DateStyle TO 'ISO, YMD'");
            }

            return $write();
        });
    }

    public function deletePassType(string $id): void
    {
        $this->deleteRecord('pass_types', $id);
    }

    /**
     * @param array<string,mixed> $row
     * @return PassTypeDocument
     */
    private function passType(array $row): array
    {
        $type = [
            'id' => $this->id($row['id']), 'name' => self::text($row['name']), 'kind' => self::text($row['kind']),
            'validityDays' => self::number($row['validity_days']), 'price' => self::number($row['price']),
            'vatRate' => self::number($row['vat_rate_bp']), 'active' => in_array($row['active'], [true, 't', 1, '1'], true),
            'createdAt' => Instant::format(Database::instant(self::text($row['created_at']))),
        ];
        if ($row['visits'] !== null) {
            $type['visits'] = self::number($row['visits']);
        }

        return $type;
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
     * @return PersonMatch
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
