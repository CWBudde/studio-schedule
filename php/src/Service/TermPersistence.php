<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use StudioSchedule\Domain\Date;

/** @phpstan-import-type TermRow from LessonGenerationPersistence */
interface TermPersistence extends LessonGenerationPersistence
{
    /** @return TermRow */
    public function lockTerm(string $id): array;

    public function saveTerm(?string $id, string $name, Date $startsOn, Date $endsOn): string;

    public function deleteTerm(string $id): void;

    /** @return list<array{id: string, slot_date: string, started: bool}> */
    public function lockExcludedLessons(string $class, Date $startsOn, Date $endsOn, DateTimeImmutable $now): array;

    public function hasOwnBookings(string $lesson): bool;

    public function deleteClassLesson(string $lesson): void;
}
