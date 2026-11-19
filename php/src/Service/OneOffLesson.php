<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateInterval;
use Psr\Clock\ClockInterface;

final readonly class OneOffLesson
{
    public function __construct(private OneOffLessonPersistence $store, private ClockInterface $clock) {}

    /** @return array<string,mixed> */
    public function create(OneOffLessonData $data): array
    {
        $data->validate();

        return $this->store->writeOneOff(function () use ($data): array {
            // Settings and lesson generation share this lock. The time zone and room
            // overlap check stay current until the lesson has been stored.
            try {
                $rules = $this->store->lockRules();
            } catch (SettingsNotFound) {
                throw new CatalogFailure(404, 'resource not found');
            }
            $start = $data->date->at($data->startTime, $rules->timezone);
            $end = $start->add(new DateInterval("PT{$data->durationMinutes}M"));
            if ($start->format('Y-m-d H:i:s') === '0001-01-01 00:00:00' || (int) $start->format('Y') > 9999) {
                throw CatalogFailure::field('date', 'the lesson start is outside the supported UTC date range');
            }
            if ((int) $end->format('Y') > 9999) {
                throw CatalogFailure::field('durationMinutes', 'the lesson end is outside the supported UTC date range');
            }
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
        });
    }
}
