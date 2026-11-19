<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Domain\ScheduleRules;

/**
 * @phpstan-import-type LevelRef from PublishedLesson
 * @phpstan-import-type WaitlistEntry from SchedulePersistence
 */
final readonly class ScheduleService
{
    public function __construct(private SchedulePersistence $store, private ClockInterface $clock) {}

    public static function dateRangeError(Date $from, Date $to): ?string
    {
        $days = $from->daysUntil($to);

        return match (true) {
            $days <= 0 => 'to must be after from',
            $days > 42 => 'the range must not exceed 42 days',
            default => null,
        };
    }

    /**
     * @param array{location?: string, instructor?: string, courseType?: string, level?: string} $filters
     * @return array<string, mixed>
     */
    public function schedule(Date $from, Date $to, array $filters, ?SessionPrincipal $principal): array
    {
        $now = $this->clock->now();

        return $this->store->inReadSnapshot(function () use ($from, $to, $filters, $principal, $now): array {
            $rules = $this->store->rules();
            $release = $rules->releaseWindow($now);
            $visibleUntil = self::visibleUntil($principal, $rules, $release['releasedUntil'], $now);
            $lessons = $this->store->lessons($rules->midnight($from), $rules->midnight($to), $filters);
            $visible = array_values(array_filter(
                $lessons,
                static fn(PublishedLesson $lesson): bool => self::isVisible($lesson, $principal, $visibleUntil),
            ));

            $schedule = [
                'timezone' => $rules->timezone->getName(),
                'now' => self::instant($now),
                'from' => (string) $from,
                'to' => (string) $to,
                'releasedUntil' => self::instant($release['releasedUntil']),
                'nextReleaseAt' => self::instant($release['nextReleaseAt']),
                'lessons' => $this->withAvailability($visible, $rules, $now, $principal),
            ];
            if ($visibleUntil !== null) {
                $schedule['visibleUntil'] = self::instant($visibleUntil);
            }

            return $schedule;
        });
    }

    /** @return array<string, mixed>|null */
    public function lesson(string $id, ?SessionPrincipal $principal): ?array
    {
        $now = $this->clock->now();

        return $this->store->inReadSnapshot(function () use ($id, $principal, $now): ?array {
            $rules = $this->store->rules();
            $lesson = $this->store->lesson($id);
            $released = $rules->releaseWindow($now)['releasedUntil'];
            $visibleUntil = self::visibleUntil($principal, $rules, $released, $now);
            if ($lesson === null || !self::isVisible($lesson, $principal, $visibleUntil)) {
                return null;
            }

            $forOffice = in_array($principal?->role, ['office', 'admin'], true);
            $body = $this->withAvailability([$lesson], $rules, $now, $principal, $forOffice)[0];

            return $body + self::lessonDescriptions($lesson);
        });
    }

    public static function visibleUntil(
        ?SessionPrincipal $principal,
        ScheduleRules $rules,
        DateTimeImmutable $released,
        DateTimeImmutable $now,
    ): ?DateTimeImmutable {
        return match ($principal?->role) {
            'admin', 'office', 'instructor' => null,
            'member' => max($released, $rules->earlyUntil($now, $principal->visibleDays)),
            default => $released,
        };
    }

    public static function isVisible(PublishedLesson $lesson, ?SessionPrincipal $principal, ?DateTimeImmutable $until): bool
    {
        if ($until === null) {
            return true;
        }

        return $lesson->eventStatus !== 'draft'
            && ($lesson->visibility === 'public' || ($principal?->role === 'member' && $lesson->visibility === 'members'))
            && ($lesson->kind !== 'regular' || $lesson->startsAt < $until);
    }

    /**
     * @param list<PublishedLesson> $lessons
     * @return list<array<string, mixed>>
     */
    private function withAvailability(
        array $lessons,
        ScheduleRules $rules,
        DateTimeImmutable $now,
        ?SessionPrincipal $principal,
        bool $forOffice = false,
    ): array {
        if ($lessons === []) {
            return [];
        }

        $ids = array_map(static fn(PublishedLesson $lesson): string => $lesson->id, $lessons);
        $events = [];
        foreach ($lessons as $lesson) {
            if ($lesson->eventId !== null) {
                $events[$lesson->eventId] = true;
            }
        }

        $active = $this->store->activeBookings($ids);
        $waitlist = $this->store->waitlist($ids);
        $eventPlaces = $events === [] ? [] : $this->store->occupiedEventPlaces(array_keys($events), $now);
        $family = $principal === null ? [] : $this->store->family($principal->personId);
        $familyBookings = $family === [] ? [] : $this->store->familyBookings($ids, array_keys($family));

        $out = [];
        foreach ($lessons as $lesson) {
            $id = $lesson->id;
            $held = 0;
            $position = 0;
            $byPerson = [];
            foreach ($waitlist[$id] ?? [] as $entry) {
                $personId = $entry['personId'];
                $status = $entry['status'];
                $expiresAt = $entry['expiresAt'];
                $entryPosition = null;
                if ($status === 'waiting') {
                    $entryPosition = ++$position;
                } elseif ($expiresAt !== null && $now < $expiresAt) {
                    ++$held;
                } else {
                    $status = 'expired';
                }

                if (isset($family[$personId])) {
                    $byPerson[$personId] = [
                        'personId' => $personId,
                        'name' => $family[$personId],
                        'entryId' => $entry['id'],
                        'status' => $status,
                        'stayOnList' => $entry['stayOnList'],
                    ] + self::present([
                        'position' => $entryPosition,
                        'expiresAt' => $entry['status'] === 'invited' && $expiresAt !== null
                            ? self::instant($expiresAt) : null,
                    ]);
                }
            }

            $bookedFor = [];
            $familyWaitlist = [];
            foreach ($family as $personId => $name) {
                foreach ($familyBookings[$id][$personId] ?? [] as $bookingId) {
                    $bookedFor[] = ['personId' => $personId, 'name' => $name, 'bookingId' => $bookingId];
                }
                if (isset($byPerson[$personId])) {
                    $familyWaitlist[] = $byPerson[$personId];
                }
            }

            $activeBookings = $active[$id] ?? 0;
            $occupied = $lesson->eventId === null ? $activeBookings + $held : ($eventPlaces[$lesson->eventId] ?? 0);
            $body = self::lessonSummary($lesson) + self::availability($lesson, $rules, $occupied) + self::present([
                'bookedFor' => $bookedFor === [] ? null : $bookedFor,
                'waitlist' => $familyWaitlist === [] ? null : $familyWaitlist,
                'admin' => $forOffice ? $this->admin($id, $activeBookings) : null,
            ]);
            $out[] = $body;
        }

        return $out;
    }

    /** @param list<WaitlistEntry> $entries */
    public static function heldPlaces(array $entries, DateTimeImmutable $now): int
    {
        return count(array_filter($entries, static fn(array $entry): bool
            => $entry['status'] === 'invited' && $entry['expiresAt'] !== null && $entry['expiresAt'] > $now));
    }

    /** @return array<string,mixed> */
    public function officeWriteDocument(PublishedLesson $lesson, ScheduleRules $rules): array
    {
        $id = $lesson->id;
        $now = $this->clock->now();
        $active = $this->store->activeBookings([$id])[$id] ?? 0;
        $held = self::heldPlaces($this->store->waitlist([$id])[$id] ?? [], $now);

        $summary = self::lessonSummary($lesson);
        unset($summary['eventSlug']);

        return $summary + self::availability($lesson, $rules, $active + $held)
            + self::lessonDescriptions($lesson) + ['admin' => $this->admin($id, $active)];
    }

    /** @return array<string, mixed> */
    private function admin(string $lessonId, int $activeBookings): array
    {
        $admin = $this->store->lessonAdmin($lessonId);

        return [
            'detached' => $admin['detached'],
            'activeBookings' => $activeBookings,
        ] + self::present([
            'cancelNote' => $admin['cancelNote'],
            'canceledAt' => $admin['canceledAt'] === null ? null : self::instant($admin['canceledAt']),
            'canceledBy' => $admin['canceledBy'],
        ]);
    }

    /** @return array<string, mixed> */
    public static function availability(PublishedLesson $lesson, ScheduleRules $rules, int $occupied): array
    {
        return [
            'freePlaces' => max(0, $lesson->capacity - $occupied),
            'bookingClosesAt' => self::cutoff($lesson->startsAt, $rules->bookingCutoffMicros),
        ] + self::present([
            'lateCancelFrom' => $lesson->status === 'canceled' ? null : self::cutoff($lesson->startsAt, $rules->cancellationCutoffMicros),
        ]);
    }

    /** @return array<string, mixed> */
    public static function lessonDescriptions(PublishedLesson $lesson): array
    {
        return self::present([
            'courseTypeDescription' => $lesson->courseTypeDescription,
            'locationAddress' => $lesson->locationAddress,
        ]);
    }

    /** @return array<string, mixed> */
    public static function lessonSummary(PublishedLesson $lesson): array
    {
        return [
            'id' => $lesson->id,
            'startsAt' => self::instant($lesson->startsAt),
            'endsAt' => self::instant($lesson->endsAt),
            'status' => $lesson->status,
            'kind' => $lesson->kind,
            'visibility' => $lesson->visibility,
            'capacity' => $lesson->capacity,
            'room' => $lesson->room,
            'location' => $lesson->location,
        ] + self::present([
            'classId' => $lesson->classId,
            'courseType' => $lesson->courseType,
            'instructor' => $lesson->instructor,
            'accompanist' => $lesson->accompanist,
        ]) + self::audience($lesson) + self::present([
            'renter' => $lesson->renter,
            'eventId' => $lesson->eventId,
            'eventSlug' => $lesson->eventSlug,
        ]);
    }

    /** @return array<string, mixed> */
    public static function audience(PublishedLesson $lesson): array
    {
        return self::present([
            'levelFrom' => self::levelRef($lesson->levelFrom),
            'levelTo' => self::levelRef($lesson->levelTo),
            'allLevels' => $lesson->allLevels ? true : null,
            'stage' => $lesson->stage,
            'ageFromMonths' => $lesson->ageFromMonths,
            'ageToMonths' => $lesson->ageToMonths,
        ]);
    }

    /**
     * @param LevelRef|null $level
     * @return array<string, mixed>|null
     */
    private static function levelRef(?array $level): ?array
    {
        return $level === null ? null : self::present([
            'id' => $level['id'],
            'name' => $level['name'],
            'code' => $level['code'],
        ]);
    }

    private static function cutoff(DateTimeImmutable $startsAt, int $micros): string
    {
        return self::instant($startsAt->modify("-$micros microseconds"));
    }

    public static function instant(DateTimeImmutable $instant): string
    {
        return Instant::format($instant);
    }

    /**
     * @param array<string, mixed> $fields
     * @return array<string, mixed>
     */
    private static function present(array $fields): array
    {
        return array_filter($fields, static fn(mixed $value): bool => $value !== null && $value !== '');
    }
}
