<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use StudioSchedule\Domain\CatalogResource;

/**
 * @phpstan-import-type CatalogData from CatalogAdminPersistence
 * @phpstan-import-type CatalogDocument from CatalogAdminPersistence
 */
final readonly class CatalogAdmin
{
    public function __construct(private CatalogAdminPersistence $store) {}

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
