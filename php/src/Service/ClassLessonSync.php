<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use RuntimeException;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\ScheduleRules;
use StudioSchedule\Domain\TimeOfDay;

/**
 * @phpstan-import-type ClassRow from LessonGenerationPersistence
 * @phpstan-import-type TermRow from LessonGenerationPersistence
 * @phpstan-import-type ScheduledClassLesson from TimetableAdminPersistence
 */
final readonly class ClassLessonSync
{
    public function __construct(
        private TimetableAdminPersistence $timetable,
        private SchedulePersistence $schedule,
        private LessonWritePersistence $writes,
        private EventWaitlistPersistence $recipients,
        private LessonWaitlistOffers $offers,
        private ClockInterface $clock,
    ) {}

    /**
     * Reconcile existing lessons while the term and class are locked.
     * @param ClassRow $old
     * @param ClassRow $class
     * @param TermRow $term
     */
    public function reconcile(array $old, array $class, array $term, ScheduleRules $rules): bool
    {
        $zone = $rules->timezone;
        $oldTime = TimeOfDay::parse(substr($old['start_time'], 0, 5));
        $newTime = TimeOfDay::parse(substr($class['start_time'], 0, 5));
        $first = Date::parse($term['starts_on']);
        $last = Date::parse($term['ends_on']);
        $today = Date::fromInstant($this->clock->now(), $zone);
        $lessons = $this->timetable->lockClassEditLessons($class['id'], $today);
        // A lesson can start while its row lock is held by another request.
        $now = $this->clock->now();
        $followers = $occupied = [];
        foreach ($lessons as $lesson) {
            $slot = Date::parse($lesson['slot_date']);
            $starts = self::instant($lesson['starts_at']);
            if ($starts > $now && self::follows($lesson, $old, $slot, $slot->at($oldTime, $zone), $starts)) {
                $followers[] = [$lesson, $slot, $starts];
            } else {
                $occupied[(string) LessonGenerator::weekOf($slot)] = true;
            }
        }

        $shift = (((int) $class['weekday'] + 6) % 7) - (((int) $old['weekday'] + 6) % 7);
        $duration = (int) $class['duration_micros'];
        $queued = false;
        $settings = null;
        foreach ($followers as [$lesson, $slot, $oldStart]) {
            $now = $this->clock->now();
            if ($oldStart <= $now) {
                $occupied[(string) LessonGenerator::weekOf($slot)] = true;
                continue;
            }
            $newSlot = $slot->addDays($shift);
            $newStart = $newSlot->at($newTime, $zone);
            $week = (string) LessonGenerator::weekOf($newSlot);
            if ($class['retired'] || isset($occupied[$week]) || $newStart <= $now
                || $newSlot->compare($first) < 0 || $newSlot->compare($last) > 0) {
                if (!$this->timetable->hasOwnBookings($lesson['id'])) {
                    $this->timetable->deleteClassLesson($lesson['id']);
                }
                continue;
            }

            $this->checkCapacity($lesson, (int) $class['capacity'], $now);
            $this->timetable->moveClassLesson($lesson['id'], $class, $newSlot, $newStart, $newStart->modify("+$duration microseconds"));
            $date = Date::fromInstant($newStart, $zone);
            if ($date->compare(Date::fromInstant($oldStart, $zone)) !== 0) {
                $this->writes->recheckPayments($lesson['lessonId'], (string) $date, $now);
            }
            if ((int) $class['capacity'] > (int) $lesson['capacity']) {
                $published = $this->schedule->lesson($lesson['lessonId']) ?? throw new RuntimeException('The moved lesson was not stored');
                $settings ??= $this->recipients->settings();
                $queued = $this->offers->offerLocked($published, $rules, $settings, $now) || $queued;
            }
            $occupied[$week] = true;
        }

        return $queued;
    }

    /**
     * @param ScheduledClassLesson $lesson
     * @param ClassRow $class
     */
    private static function follows(array $lesson, array $class, Date $slot, DateTimeImmutable $expected, DateTimeImmutable $actual): bool
    {
        $duration = (int) $class['duration_micros'];

        return !$lesson['detached'] && $slot->weekday() === (int) $class['weekday']
            && $lesson['status'] === 'scheduled' && $lesson['visibility'] === 'public' && $lesson['kind'] === 'regular'
            && $actual == $expected && self::instant($lesson['ends_at']) == $expected->modify("+$duration microseconds")
            && $lesson['course_type_id'] === $class['course_type_id'] && $lesson['room_id'] === $class['room_id']
            && $lesson['instructor_id'] === $class['instructor_id'] && $lesson['accompanist_id'] === $class['accompanist_id']
            && (int) $lesson['capacity'] === (int) $class['capacity'];
    }

    /** @param ScheduledClassLesson $lesson */
    private function checkCapacity(array $lesson, int $capacity, DateTimeImmutable $now): void
    {
        if ($capacity >= (int) $lesson['capacity']) {
            return;
        }
        $id = $lesson['lessonId'];
        $booked = $this->schedule->activeBookings([$id])[$id] ?? 0;
        $held = ScheduleService::heldPlaces($this->schedule->waitlist([$id])[$id] ?? [], $now);
        if ($booked + $held > $capacity) {
            $reserved = $held === 0 ? '' : " and $held places reserved for the waiting list";
            throw CatalogFailure::field('capacity', "the lesson of {$lesson['slot_date']} has $booked bookings$reserved, more than the capacity");
        }
    }

    private static function instant(string $value): DateTimeImmutable
    {
        return new DateTimeImmutable($value, new DateTimeZone('UTC'));
    }
}
