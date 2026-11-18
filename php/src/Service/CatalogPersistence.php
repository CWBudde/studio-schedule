<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

/** @phpstan-type CatalogRow array<string, string|int|null> */
interface CatalogPersistence
{
    /** @return list<CatalogRow> */
    public function locations(): array;

    /** @return list<CatalogRow>|null Null means the selected location does not exist. */
    public function rooms(?string $location): ?array;

    /** @return list<CatalogRow> */
    public function staff(): array;

    /** @return list<CatalogRow> */
    public function courseTypes(): array;

    /** @return list<CatalogRow> */
    public function levels(): array;

    /** @return list<CatalogRow> */
    public function terms(): array;
}
