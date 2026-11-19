<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\TimeOfDay;

/**
 * @phpstan-import-type ClassRow from LessonGenerationPersistence
 * @phpstan-import-type TermRow from LessonGenerationPersistence
 */
final readonly class LessonGenerator
{
    public function __construct(private LessonGenerationPersistence $store, private ClockInterface $clock) {}

    public function generateAhead(): int
    {
        return $this->store->write(function (): int {
            $rules = $this->store->lockRules();
            $now = $this->clock->now();
            $from = Date::fromInstant($now, $rules->timezone);
            $until = $rules->generationEnd($from, $this->store->longestVisibleDays());
            $created = 0;
            foreach ($this->store->lockTerms($from, $until) as $term) {
                foreach ($this->store->lockClasses($term['id']) as $class) {
                    $created += $this->fillLockedClass($class, $term, $from, $until, $rules->timezone, $now);
                }
            }

            return $created;
        });
    }

    /**
     * The caller holds the term and class locks in the current transaction.
     * @param ClassRow $class
     * @param TermRow $term
     */
    public function fillLockedClass(array $class, array $term, Date $from, Date $until, DateTimeZone $zone, DateTimeImmutable $after): int
    {
        $from = max($from, Date::parse($term['starts_on']));
        $lastDay = Date::parse($term['ends_on']);
        $until = $lastDay->compare($until) >= 0 ? $until : $lastDay->addDays(1);
        if ($class['retired'] || $from->compare($until) >= 0) {
            return 0;
        }

        return $this->fill($class, $from, $until, $zone, $after);
    }

    /** @param ClassRow $class */
    private function fill(array $class, Date $from, Date $until, DateTimeZone $zone, DateTimeImmutable $after): int
    {
        $date = $from->addDays(((int) $class['weekday'] - $from->weekday() + 7) % 7);
        if ($date->compare($until) >= 0) {
            return 0;
        }
        $weeks = [];
        foreach ($this->store->lockSlotDates($class['id'], self::weekOf($date)) as $slot) {
            $weeks[(string) self::weekOf(Date::parse($slot))] = true;
        }
        $time = TimeOfDay::parse(substr($class['start_time'], 0, 5));
        $duration = (int) $class['duration_micros'];
        $now = $this->clock->now();
        $members = null;
        $created = 0;
        for (; $date->compare($until) < 0; $date = $date->addDays(7)) {
            $start = $date->at($time, $zone);
            if (isset($weeks[(string) self::weekOf($date)]) || $start <= $after) {
                continue;
            }
            $lesson = $this->store->createLesson($class, $date, $start, $start->modify("+$duration microseconds"));
            ++$created;
            if ($start <= $now) {
                continue;
            }
            $members ??= $this->store->lockMembers($class['id']);
            $taken = 0;
            foreach ($members as $member) {
                if (($member['since'] === null || $member['since'] <= (string) $date)
                    && ($member['paused_until'] === null || $member['paused_until'] < (string) $date)) {
                    $this->store->createMemberBooking($lesson, $member['person_id'], $now, $taken >= (int) $class['capacity']);
                    ++$taken;
                }
            }
        }

        return $created;
    }

    private static function weekOf(Date $date): Date
    {
        return $date->addDays(-(($date->weekday() + 6) % 7));
    }
}
