<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

/**
 * @phpstan-import-type CatalogDocument from CatalogAdminPersistence
 * @phpstan-type CatalogRow array<string, string|int|null>
 * @phpstan-type PersonMatch array{id: string, givenName: string, familyName: string, email?: string, memberNo?: string, anonymized?: true}
 * @phpstan-type RosterRow array{person: PersonMatch, since: ?string, pausedUntil: ?string}
 */
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

    /** @return list<CatalogDocument>|null Null means the term does not exist. */
    public function classes(string $term, bool $withRetired): ?array;

    /** @return array{timezone: ?string, members: list<RosterRow>}|null Null means the class does not exist. */
    public function classMembers(string $class): ?array;
}
