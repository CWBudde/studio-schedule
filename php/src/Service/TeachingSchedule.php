<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use InvalidArgumentException;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Domain\PublishedLesson;

final readonly class TeachingSchedule
{
    public function __construct(private SchedulePersistence $store, private ClockInterface $clock) {}

    /** @return array{timezone:string,now:string,from:string,to:string,lessons:list<array<string,mixed>>} */
    public function view(SessionPrincipal $principal, ?Date $from, ?Date $to, ?string $staff): array
    {
        $now = $this->clock->now();

        return $this->store->inReadSnapshot(function () use ($principal, $from, $to, $staff, $now): array {
            $rules = $this->store->rules();
            try {
                $from ??= Date::fromInstant($now, $rules->timezone)->addDays(-7);
                $to ??= $from->addDays(15);
            } catch (InvalidArgumentException) {
                throw new CatalogFailure(422, 'invalid date');
            }
            $days = $from->daysUntil($to);
            if ($days <= 0 || $days > 31) {
                throw new CatalogFailure(422, $days <= 0 ? 'to must be after from' : 'the range must not exceed 31 days');
            }

            $office = in_array($principal->role, ['office', 'admin'], true);
            $own = $this->store->activeStaff($principal->personId);
            $selected = $office ? null : array_keys($own);
            if ($staff !== null) {
                $selected = $office || isset($own[$staff]) ? [$staff] : [];
            }
            $lessons = $selected === [] ? [] : $this->store->teachingLessons(
                $rules->midnight($from),
                $rules->midnight($to),
                $selected,
                storedCapacity: true,
            );
            $bookable = array_values(array_filter($lessons, static fn(PublishedLesson $lesson): bool => $lesson->kind !== 'rental'));

            return [
                'timezone' => $rules->timezone->getName(), 'now' => Instant::format($now),
                'from' => (string) $from, 'to' => (string) $to,
                'lessons' => $this->summaries($bookable, $rules->timezone, $now, $office, $own),
            ];
        });
    }

    /**
     * @param list<PublishedLesson> $lessons
     * @param array<string,true> $own
     * @return list<array<string,mixed>>
     */
    private function summaries(array $lessons, DateTimeZone $zone, DateTimeImmutable $now, bool $office, array $own): array
    {
        if ($lessons === []) {
            return [];
        }

        $ids = array_map(static fn(PublishedLesson $lesson): string => $lesson->id, $lessons);
        $bookings = $this->store->bookingCounts($ids);
        $waitlists = $this->store->waitlist($ids);
        $summaries = [];
        foreach ($lessons as $lesson) {
            $count = $bookings[$lesson->id] ?? ['active' => 0, 'booked' => 0, 'trials' => 0, 'guests' => 0];
            $entries = $waitlists[$lesson->id] ?? [];
            $held = ScheduleService::heldPlaces($entries, $now);
            $dayOver = ScheduleService::dayOverAt($lesson->startsAt, $lesson->endsAt, $zone);
            $started = $lesson->status !== 'canceled' && $now >= $lesson->startsAt;
            $role = match (true) {
                isset($own[$lesson->instructor['id'] ?? '']) => 'instructor',
                isset($own[$lesson->accompanist['id'] ?? '']) => 'accompanist',
                default => 'office',
            };

            $summaries[] = ScheduleService::lessonSummary($lesson) + [
                'role' => $role, 'active' => $count['active'], 'held' => $held,
                'freePlaces' => max(0, $lesson->capacity - $count['active'] - $held),
                'waiting' => count(array_filter($entries, static fn(array $entry): bool => $entry['status'] === 'waiting')),
                'trials' => $count['trials'], 'guests' => $count['guests'],
                'dayOverAt' => Instant::format($dayOver), 'attendanceOpen' => $started && ($office || $now < $dayOver),
                'attendanceTaken' => $started && $count['booked'] === 0,
            ];
        }

        return $summaries;
    }
}
