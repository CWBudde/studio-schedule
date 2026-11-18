<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

final readonly class CatalogService
{
    public const array COLLECTIONS = ['locations', 'rooms', 'staff', 'course-types', 'levels', 'terms'];

    public function __construct(private CatalogPersistence $store) {}

    /**
     * @param value-of<self::COLLECTIONS> $collection
     * @return list<array<string, string|int>>|null
     */
    public function items(string $collection, ?string $location = null): ?array
    {
        [$rows, $order] = match ($collection) {
            'locations' => [$this->store->locations(), ['name', 'id']],
            'rooms' => [$this->store->rooms($location), ['locationName', 'locationId', 'position', 'name', 'id']],
            'staff' => [$this->store->staff(), ['displayName', 'id']],
            'course-types' => [$this->store->courseTypes(), ['name', 'id']],
            'levels' => [$this->store->levels(), ['position', 'name', 'id']],
            'terms' => [$this->store->terms(), ['startsOn', 'id']],
        };
        if ($rows === null) {
            return null;
        }

        // PostgreSQL's C collation orders these strings by bytes. Compare
        // complete strings on every engine, including long names on MySQL.
        usort($rows, static function (array $a, array $b) use ($order): int {
            foreach ($order as $field) {
                $left = $a[$field];
                $right = $b[$field];
                $comparison = is_int($left) && is_int($right) ? $left <=> $right : strcmp((string) $left, (string) $right);
                if ($comparison !== 0) {
                    return $comparison;
                }
            }

            return 0;
        });

        return array_map(static function (array $row): array {
            unset($row['locationName']);

            return array_filter($row, static fn(string|int|null $value): bool => $value !== null && $value !== '');
        }, $rows);
    }
}
