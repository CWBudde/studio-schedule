<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\CatalogResource;

/**
 * @phpstan-import-type CatalogData from CatalogAdminPersistence
 * @phpstan-import-type CatalogDocument from CatalogAdminPersistence
 * @phpstan-import-type PassTypeData from CatalogAdminPersistence
 * @phpstan-import-type PassTypeDocument from CatalogAdminPersistence
 */
final readonly class CatalogAdmin
{
    public function __construct(private CatalogAdminPersistence $store, private ClockInterface $clock) {}

    /** @return list<CatalogDocument> */
    public function allStaff(): array
    {
        return $this->store->allStaff();
    }

    /**
     * @param CatalogData $body
     * @return CatalogDocument
     */
    public function save(CatalogResource $resource, ?string $id, array $body): array
    {
        return $this->store->save($resource, $id, static function () use ($resource, $body): array {
            $required = $resource === CatalogResource::Staff ? 'displayName' : 'name';
            foreach (['name', 'address', 'description', 'code', 'displayName'] as $field) {
                $value = $body[$field] ?? null;
                if (is_string($value)) {
                    $trimmed = SetupInput::trim($value);
                    $body[$field] = $trimmed === '' ? null : $trimmed;
                }
            }
            if (($body[$required] ?? null) === null) {
                throw CatalogFailure::field($required, 'name must not be empty');
            }

            return $body;
        });
    }

    public function delete(CatalogResource $resource, string $id): void
    {
        $this->store->delete($resource, $id);
    }

    /** @return list<PassTypeDocument> */
    public function passTypes(): array
    {
        return $this->store->passTypes();
    }

    /**
     * @param PassTypeData $body
     * @return PassTypeDocument
     */
    public function savePassType(?string $id, array $body): array
    {
        return $this->store->savePassType($id, static function () use ($body): array {
            $body['name'] = SetupInput::trim($body['name']);
            if ($body['name'] === '') {
                throw CatalogFailure::field('name', 'pass type name must not be empty');
            }
            if ($body['kind'] === 'visits' && $body['visits'] === 0) {
                throw CatalogFailure::field('visits', 'visits must be between 1 and 1000');
            }
            if ($body['kind'] === 'time' && $body['visits'] !== 0) {
                throw CatalogFailure::field('visits', 'a time pass has no number of visits');
            }
            if ($body['kind'] === 'time' && $body['validityDays'] === 0) {
                throw CatalogFailure::field('validityDays', 'a time pass lasts at least one day');
            }

            return $body;
        }, $this->clock->now());
    }

    /** @return PassTypeDocument */
    public function retirePassType(string $id): array
    {
        return $this->store->retirePassType($id);
    }

    public function deletePassType(string $id): void
    {
        $this->store->deletePassType($id);
    }

    /** @param list<string> $ids */
    public function orderRooms(string $location, array $ids): void
    {
        $this->store->orderRooms($location, $ids);
    }

    /** @param list<string> $ids */
    public function orderLevels(array $ids): void
    {
        $this->store->orderLevels($ids);
    }
}
