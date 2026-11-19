<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use DateTimeZone;

/**
 * @phpstan-type ImportPerson array{
 *   id:string,memberNo:string,givenName:string,familyName:string,email:string,
 *   phone:string,birthDate:string,notes:string,accountEmail:string
 * }
 * @phpstan-type Counts array{
 *   rows:int,created:int,updated:int,unchanged:int,failed:int,guardiansCreated:int,guardiansUpdated:int,linksAdded:int
 * }
 * @phpstan-type RunError array{line:int,code:string,field:string}
 */
interface MemberImportPersistence
{
    /**
     * @template T
     * @param Closure():T $write
     * @return T
     */
    public function write(Closure $write): mixed;

    /** @return list<ImportPerson> */
    public function people(): array;

    /** @return list<ImportPerson> */
    public function searchPeople(): array;

    /**
     * @param list<string> $ids
     * @return list<string>
     */
    public function lockPeople(array $ids): array;

    public function schoolZone(): DateTimeZone;

    /** @param ImportPerson $person */
    public function save(array $person, bool $create): void;

    public function linked(string $guardian, string $member): bool;

    public function link(string $guardian, string $member): void;

    /** @return list<string> */
    public function dependents(string $person): array;

    /**
     * @param Counts $counts
     * @param list<RunError> $errors
     */
    public function record(string $name, ?string $actor, DateTimeImmutable $start, DateTimeImmutable $end, array $counts, array $errors): string;

    /** @return list<array<string,mixed>> */
    public function runs(): array;
}
