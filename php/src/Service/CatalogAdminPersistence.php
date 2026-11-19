<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use StudioSchedule\Domain\CatalogResource;

/**
 * @phpstan-type CatalogData array<string, string|int|bool|null>
 * @phpstan-type CatalogDocument array<string, string|int|bool|array<string,string|bool>>
 */
interface CatalogAdminPersistence
{
    /** @return list<CatalogDocument> */
    public function allStaff(): array;

    /**
     * @param Closure():CatalogData $data
     * @return CatalogDocument
     */
    public function save(CatalogResource $resource, ?string $id, Closure $data): array;

    public function delete(CatalogResource $resource, string $id): void;

    /** @param list<string> $ids */
    public function orderRooms(string $location, array $ids): void;

    /** @param list<string> $ids */
    public function orderLevels(array $ids): void;
}
