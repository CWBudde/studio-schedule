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
 * @phpstan-type ScheduledClassLesson array{id:string, lessonId:string, slot_date:string, starts_at:string, ends_at:string,
 *     status:string, visibility:string, kind:string, detached:bool, course_type_id:?string, room_id:string,
 *     instructor_id:?string, accompanist_id:?string, capacity:int|numeric-string}
 */
interface TimetableAdminPersistence extends LessonGenerationPersistence
{
    /** @return TermRow */
    public function lockTerm(string $id): array;

    public function saveTerm(?string $id, string $name, Date $startsOn, Date $endsOn): string;

    public function deleteTerm(string $id): void;

    /** @return ClassRow */
    public function saveClass(string $term, ClassData $data, ?string $storedId = null): array;

    public function classTerm(string $classId): string;

    /** @return ClassRow */
    public function lockClass(string $classId): array;

    /** @return list<ScheduledClassLesson> */
    public function lockClassEditLessons(string $class, Date $from): array;

    /** @param ClassRow $class */
    public function moveClassLesson(string $lesson, array $class, Date $slot, DateTimeImmutable $start, DateTimeImmutable $end): void;

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
