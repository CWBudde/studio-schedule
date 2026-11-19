<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Domain\ScheduleRules;

/** @phpstan-import-type FamilyBooking from MyLessonsPersistence */
final readonly class MyLessons
{
    public function __construct(
        private MyLessonsPersistence $store,
        private SchedulePersistence $schedule,
        private ClockInterface $clock,
    ) {}

    /** @return array<string,mixed> */
    public function upcoming(SessionPrincipal $principal): array
    {
        $now = $this->clock->now();

        return $this->schedule->inReadSnapshot(function () use ($principal, $now): array {
            $rules = $this->schedule->rules();
            $family = $this->schedule->family($principal->personId);
            $people = array_keys($family);
            $bookings = $this->store->upcoming($people, $now);
            $waiting = $this->store->waitingLessonIds($people, $now);
            $lessons = $this->findLessons($bookings, $waiting);

            return [
                'timezone' => $rules->timezone->getName(), 'now' => Instant::format($now),
                'upcoming' => $this->bookings($bookings, $lessons, $family, $rules, $now),
                'waitlist' => $this->waitlist($waiting, $lessons, $family, $rules, $now),
            ];
        });
    }

    /** @return array<string,mixed> */
    public function history(SessionPrincipal $principal, ?DateTimeImmutable $before, string $nanos): array
    {
        $now = $this->clock->now();
        if ($before === null || $before > $now || ($before == $now && $nanos !== '')) {
            $before = $now;
            $nanos = '';
        }

        return $this->schedule->inReadSnapshot(function () use ($principal, $now, $before, $nanos): array {
            $rules = $this->schedule->rules();
            $from = self::historyStart($before, $rules->timezone);
            $family = $this->schedule->family($principal->personId);
            $people = array_keys($family);
            $bookings = $this->store->history($people, $now, $from, $before);
            $lessons = $this->findLessons($bookings, []);

            return [
                'timezone' => $rules->timezone->getName(), 'now' => Instant::format($now),
                'before' => self::cursor($before, $nanos), 'nextBefore' => self::cursor($from, $nanos),
                'items' => $this->bookings($bookings, $lessons, $family, $rules, $now),
                'hasMore' => $this->store->hasHistoryBefore($people, $now, $from),
            ];
        });
    }

    private static function cursor(DateTimeImmutable $instant, string $nanos): string
    {
        if ($nanos === '') {
            return Instant::format($instant);
        }

        $microseconds = $instant->setTimezone(new DateTimeZone('UTC'))->format('Y-m-d\TH:i:s.u');

        return $microseconds . $nanos . 'Z';
    }

    private static function historyStart(DateTimeImmutable $before, DateTimeZone $zone): DateTimeImmutable
    {
        $wall = new DateTimeImmutable($before->setTimezone($zone)->format('Y-m-d\TH:i:s.u') . 'Z');
        $wall = $wall->modify('-3 months');
        // Match Go's AddDate: normalize the calendar date before resolving the zone offset.
        $probe = $wall->modify(sprintf('%+d seconds', -$zone->getOffset($wall)));

        return $wall->modify(sprintf('%+d seconds', -$zone->getOffset($probe)));
    }

    /**
     * @param list<FamilyBooking> $bookings
     * @param list<string> $waiting
     * @return array<string,PublishedLesson>
     */
    private function findLessons(array $bookings, array $waiting): array
    {
        return array_column(
            $this->schedule->lessonsById(array_values(array_unique([...array_column($bookings, 'lessonId'), ...$waiting]))),
            null,
            'id',
        );
    }

    /**
     * @param list<FamilyBooking> $rows
     * @param array<string,PublishedLesson> $lessons
     * @param array<string,string> $family
     * @return list<array<string,mixed>>
     */
    private function bookings(array $rows, array $lessons, array $family, ScheduleRules $rules, DateTimeImmutable $now): array
    {
        $onBooking = $this->store->redemptionOnBooking();
        $documents = [];
        foreach ($rows as $row) {
            $lesson = $lessons[$row['lessonId']];
            $availability = ScheduleService::availability($lesson, $rules, 0);
            $booking = array_diff_key($row, ['trial' => true, 'pass' => true]);
            if ($booking['canceledAt'] === null) {
                unset($booking['canceledAt']);
            }
            $documents[] = [
                'booking' => $booking, 'personName' => $family[$row['personId']], 'lesson' => array_diff_key(ScheduleService::lessonSummary($lesson), ['eventSlug' => true]),
                'canCancel' => $row['status'] !== 'canceled' && $now < $lesson->startsAt,
                'payment' => BookingPayment::document($row, $lesson->status, $onBooking),
            ] + array_intersect_key($availability, ['lateCancelFrom' => true]);
        }

        return $documents;
    }

    /**
     * @param list<string> $ids
     * @param array<string,PublishedLesson> $lessons
     * @param array<string,string> $family
     * @return list<array<string,mixed>>
     */
    private function waitlist(array $ids, array $lessons, array $family, ScheduleRules $rules, DateTimeImmutable $now): array
    {
        if ($ids === []) {
            return [];
        }
        $all = $this->schedule->waitlist($ids);
        $documents = [];
        foreach ($lessons as $id => $lesson) {
            $position = 0;
            $entries = [];
            foreach ($all[$id] ?? [] as $row) {
                $waiting = $row['status'] === 'waiting';
                $place = $waiting ? ++$position : null;
                $person = $row['personId'];
                if (!isset($family[$person])) {
                    continue;
                }
                $expiry = $row['expiresAt'];
                $state = $row['status'] === 'invited' && ($expiry === null || $expiry <= $now) ? 'expired' : $row['status'];
                $entries[$person] = [
                    'personId' => $person, 'name' => $family[$person], 'entryId' => $row['id'], 'status' => $state,
                    'stayOnList' => $row['stayOnList'],
                ] + ($place === null ? [] : ['position' => $place])
                  + ($expiry === null ? [] : ['expiresAt' => Instant::format($expiry)]);
            }
            $availability = ScheduleService::availability($lesson, $rules, 0);
            foreach (array_intersect_key($family, $entries) as $person => $_) {
                $documents[] = [
                    'entry' => $entries[$person], 'lesson' => array_diff_key(ScheduleService::lessonSummary($lesson), ['eventSlug' => true]),
                    'bookingClosesAt' => $availability['bookingClosesAt'],
                ];
            }
        }

        return $documents;
    }
}
