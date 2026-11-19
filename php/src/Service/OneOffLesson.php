<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\ScheduleRules;

final readonly class OneOffLesson
{
    public function __construct(private OneOffLessonPersistence $store, private ClockInterface $clock) {}

    /** @return array<string,mixed> */
    public function create(OneOffLessonData $data): array
    {
        $data->validate();

        return $this->store->writeOneOff(fn(): array => $this->persist($data, $this->lockRules()));
    }

    public function lockRules(): ScheduleRules
    {
        // Settings and lesson generation share this lock, which also serializes room checks.
        try {
            return $this->store->lockRules();
        } catch (SettingsNotFound) {
            throw new CatalogFailure(404, 'resource not found');
        }
    }

    /** @return array<string,mixed> */
    public function createEventSession(OneOffLessonData $data, ScheduleRules $rules): array
    {
        $data->validate();

        return $this->persist($data, $rules);
    }

    /** @return array<string,mixed> */
    private function persist(OneOffLessonData $data, ScheduleRules $rules): array
    {
        [$start, $end] = $data->instants($rules);
        $this->store->lockLessonReferences($data);
        if (!$data->confirmPast && $start <= $this->clock->now()) {
            throw new CatalogFailure(409, 'the lesson would start in the past', reason: 'past');
        }
        if (!$data->confirmRoomOverlap) {
            $overlap = $this->store->roomOverlap($data->roomId, $start, $end);
            if ($overlap !== null) {
                throw new LessonRoomTaken($overlap);
            }
        }

        $lesson = $this->store->createOneOffLesson($data, $start, $end);

        return ScheduleService::lessonSummary($lesson)
            + ScheduleService::availability($lesson, $rules, occupied: 0)
            + ['admin' => ['activeBookings' => 0, 'detached' => false]]
            + ScheduleService::lessonDescriptions($lesson);
    }
}
