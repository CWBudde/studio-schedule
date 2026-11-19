<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\ScheduleRules;

/** @phpstan-import-type TermRow from LessonGenerationPersistence */
final readonly class TermAdmin
{
    public function __construct(
        private TimetableAdminPersistence $store,
        private LessonGenerator $generator,
        private ClockInterface $clock,
    ) {}

    /** @return array{id: string, name: string, startsOn: string, endsOn: string} */
    public function save(?string $id, string $name, Date $startsOn, Date $endsOn): array
    {
        return $this->store->write(function () use ($id, $name, $startsOn, $endsOn): array {
            // Generation and term edits both acquire settings before the term.
            $rules = $id === null ? null : $this->rules();
            $previous = $id === null ? null : $this->store->lockTerm($id);
            $name = SetupInput::trim($name);
            if ($name === '') {
                throw CatalogFailure::field('name', 'term name must not be empty');
            }
            if ($startsOn->compare($endsOn) > 0) {
                throw CatalogFailure::field('endsOn', 'term must not end before it starts');
            }
            $key = $this->store->saveTerm($id, $name, $startsOn, $endsOn);
            $start = (string) $startsOn;
            $end = (string) $endsOn;
            if ($rules !== null && $previous !== null && ($previous['starts_on'] !== $start || $previous['ends_on'] !== $end)) {
                $this->fitLessons(['id' => $previous['id'], 'starts_on' => $start, 'ends_on' => $end], $startsOn, $endsOn, $rules);
            }

            return ['id' => $key, 'name' => $name, 'startsOn' => $start, 'endsOn' => $end];
        });
    }

    public function delete(string $id): void
    {
        $this->store->write(function () use ($id): void {
            $this->store->deleteTerm($id);
        });
    }

    private function rules(): ?ScheduleRules
    {
        try {
            return $this->store->lockRules();
        } catch (SettingsNotFound) {
            return null;
        }
    }

    /** @param TermRow $term */
    private function fitLessons(array $term, Date $startsOn, Date $endsOn, ScheduleRules $rules): void
    {
        $now = $this->clock->now();
        $today = Date::fromInstant($now, $rules->timezone);
        $until = $rules->generationEnd($today, $this->store->longestVisibleDays());
        foreach ($this->store->lockClasses($term['id'], includeRetired: true) as $class) {
            $this->removeExcludedLessons($class['id'], $startsOn, $endsOn, $now);
            $this->generator->fillLockedClass($class, $term, $today, $until, $rules->timezone, $now);
        }
    }

    private function removeExcludedLessons(string $class, Date $startsOn, Date $endsOn, DateTimeImmutable $now): void
    {
        foreach ($this->store->lockExcludedLessons($class, $startsOn, $endsOn, $now) as $lesson) {
            $reason = match (true) {
                $lesson['started'] => 'has started',
                $this->store->hasOwnBookings($lesson['id']) => 'has bookings',
                default => null,
            };
            if ($reason !== null) {
                $field = $lesson['slot_date'] < (string) $startsOn ? 'startsOn' : 'endsOn';
                throw CatalogFailure::field($field, "the lesson of {$lesson['slot_date']} would lie outside the term, but it $reason");
            }
            $this->store->deleteClassLesson($lesson['id']);
        }
    }
}
