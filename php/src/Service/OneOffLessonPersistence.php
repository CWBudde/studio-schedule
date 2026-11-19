<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Domain\ScheduleRules;

interface OneOffLessonPersistence
{
    /**
     * @template T
     * @param Closure():T $operation
     * @return T
     */
    public function writeOneOff(Closure $operation): mixed;

    public function lockRules(): ScheduleRules;

    public function lockLessonReferences(OneOffLessonData $data): void;

    public function roomOverlap(string $roomId, DateTimeImmutable $start, DateTimeImmutable $end, ?string $excluding = null): ?PublishedLesson;

    public function createOneOffLesson(OneOffLessonData $data, DateTimeImmutable $start, DateTimeImmutable $end): PublishedLesson;
}
