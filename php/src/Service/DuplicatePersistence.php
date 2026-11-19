<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

/**
 * @phpstan-import-type ImportPerson from MemberImportPersistence
 */
interface DuplicatePersistence
{
    /**
     * @return array{people:list<ImportPerson>,guardians:array<string,array<string,true>>,dismissed:array<string,array<string,true>>}
     */
    public function snapshot(): array;

    public function saveDismissal(string $a, string $b, string $actor): bool;
}
