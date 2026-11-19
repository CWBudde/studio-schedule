<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use StudioSchedule\Domain\Date;

/**
 * @phpstan-import-type TermRow from LessonGenerationPersistence
 * @phpstan-import-type ClassRow from LessonGenerationPersistence
 * @phpstan-import-type CatalogDocument from CatalogAdminPersistence
 * @phpstan-type ClassLesson array{id: string, slot_date: string, started: bool}
 */
interface TimetableAdminPersistence extends LessonGenerationPersistence
{
    /** @return TermRow */
    public function lockTerm(string $id): array;

    public function saveTerm(?string $id, string $name, Date $startsOn, Date $endsOn): string;

    public function deleteTerm(string $id): void;

    /** @return ClassRow */
    public function createClass(string $term, ClassData $data): array;

    /** @return CatalogDocument */
    public function classDocument(string $id): array;

    public function lockClassId(string $id): string;

    public function deleteClass(string $id): void;

    /** @return list<ClassLesson> */
    public function lockClassLessons(string $class, DateTimeImmutable $now): array;

    /** @return list<ClassLesson> */
    public function lockExcludedLessons(string $class, Date $startsOn, Date $endsOn, DateTimeImmutable $now): array;

    public function hasOwnBookings(string $lesson): bool;

    public function deleteClassLesson(string $lesson): void;
}
