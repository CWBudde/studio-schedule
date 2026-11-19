<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use StudioSchedule\Domain\CatalogResource;

/**
 * @phpstan-type CatalogData array<string, string|int|bool|null>
 * @phpstan-type CatalogDocument array<string, string|int|bool|array<string,string|bool>>
 * @phpstan-type PassTypeData array{name:string,kind:'visits'|'time',visits:int,validityDays:int,price:int,vatRate:int,active:bool}
 * @phpstan-type PassTypeDocument array{id:string,name:string,kind:string,visits?:int,validityDays:int,price:int,vatRate:int,active:bool,createdAt:string}
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

    /** @return list<PassTypeDocument> */
    public function passTypes(): array;

    /**
     * @param Closure():PassTypeData $data
     * @return PassTypeDocument
     */
    public function savePassType(?string $id, Closure $data, DateTimeImmutable $now): array;

    /** @return PassTypeDocument */
    public function retirePassType(string $id): array;

    public function deletePassType(string $id): void;

    /** @param list<string> $ids */
    public function orderRooms(string $location, array $ids): void;

    /** @param list<string> $ids */
    public function orderLevels(array $ids): void;
}
