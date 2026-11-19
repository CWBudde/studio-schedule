<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use StudioSchedule\Domain\PersonRecord;

interface PersonAdministrationPersistence
{
    /**
     * @template T
     * @param Closure():T $write
     * @return T
     */
    public function inOfficeTransaction(Closure $write): mixed;

    public function lockLivePersons(string ...$ids): bool;

    public function hasLivePerson(string $id, bool $lock = false): bool;

    public function schoolTimezone(): string;

    public function saveOfficePerson(string $id, PersonRecord $record, bool $create): void;

    /** @return array{id:string,givenName:string,familyName:string,email?:string,memberNo?:string}|null */
    public function personMatch(string $id): ?array;

    /** @return list<string> */
    public function dependentIds(string $guardian): array;

    public function linkGuardian(string $guardian, string $dependent): void;

    public function unlinkGuardian(string $guardian, string $dependent): bool;
}
