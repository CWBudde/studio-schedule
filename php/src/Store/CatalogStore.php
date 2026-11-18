<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Service\CatalogPersistence;
use Symfony\Component\Uid\Uuid;

final readonly class CatalogStore implements CatalogPersistence
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
