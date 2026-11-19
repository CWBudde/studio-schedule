<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use RuntimeException;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Domain\TimeOfDay;
use StudioSchedule\Store\StatisticsStore;

/**
 * @phpstan-type Group array{order:list<int|string>, labels:array<string,mixed>, counts:array<string,int>}
 * @phpstan-import-type ClassSlot from StatisticsStore
 */
final readonly class StatisticsService
{
    private const int MAX_DAYS = 366;
    private const int WEEKLY_DAYS = 26 * 7;
    private const array COUNTS = [
        'lessons' => 0, 'capacity' => 0, 'bookings' => 0, 'attended' => 0, 'noShows' => 0,
        'lateCancellations' => 0, 'trials' => 0, 'guests' => 0, 'waitlistEntries' => 0,
    ];
    private const array SALES = ['passesSold' => 0, 'salesGross' => 0];

    public function __construct(
        private StatisticsStore $store,
        private SchedulePersistence $schedule,
        private ClockInterface $clock,
    ) {}

    public static function customRangeError(Date $from, Date $to): ?string
    {
        return match (true) {
            $from->isZero() || $to->isZero() => 'a custom range needs valid from and to dates',
            $from->compare($to) > 0 => 'to must not be before from',
            $from->daysUntil($to) >= self::MAX_DAYS => 'the range must not exceed ' . self::MAX_DAYS . ' days',
            default => null,
        };
    }

    /** @return array<string,mixed>|null Null when a term report has no current term. */
    public function report(string $range, Date $from, Date $to): ?array
    {
        return $this->schedule->inReadSnapshot(function () use ($range, $from, $to): ?array {
            $school = $this->store->school();
            $zone = new DateTimeZone($school['timezone']);
            $today = Date::fromInstant($this->clock->now(), $zone);
            $term = $range === 'term' ? $this->store->term($today) : null;
            if ($range === 'term' && $term === null) {
                return null;
            }
            if ($range !== 'custom') {
                $to = $today;
                $from = $term === null
                    ? $today->addDays(-self::mondayIndex($today->weekday()) - ($range === 'last12Weeks' ? 77 : 21))
                    : Date::parse($term['start']);
                if ($from->daysUntil($to) >= self::MAX_DAYS) {
                    $from = $to->addDays(1 - self::MAX_DAYS);
                }
            }
            $monthly = $from->daysUntil($to) >= self::WEEKLY_DAYS;

            return $school + [
                'today' => (string) $today, 'range' => $range, 'from' => (string) $from, 'to' => (string) $to,
                'granularity' => $monthly ? 'month' : 'week',
            ] + ($term === null ? [] : ['term' => ['id' => $term['id'], 'name' => $term['name']]])
                + $this->figures($from, $to, $zone, $school['currency'], $monthly);
        });
    }

    /** @return array<string,mixed> */
    private function figures(Date $from, Date $to, DateTimeZone $zone, string $currency, bool $monthly): array
    {
        $midnight = TimeOfDay::parse('00:00');
        $start = $from->at($midnight, $zone);
        $end = (string) $to === '9999-12-31'
            ? $to->at($midnight, $zone)->setTimezone($zone)->modify('tomorrow')
            : $to->addDays(1)->at($midnight, $zone);
        $lessons = $this->schedule->lessons($start, $end, []);
        $bookings = $this->store->bookings($start, $end);
        $waitlist = $this->store->waitlist($start, $end);
        $slots = $this->store->classSlots($start, $end);
        [$periods, $periodByDate] = self::periods($from, $to, $monthly);
        $periodCounts = array_fill(0, count($periods), self::COUNTS + self::SALES);
        $totals = self::COUNTS + self::SALES + ['canceledLessons' => 0, 'activeMembers' => 0, 'eventFees' => 0];
        $classes = $rooms = $courseTypes = $instructors = $oneOffs = [];
        foreach ($lessons as $lesson) {
            if ($lesson->eventId !== null || $lesson->kind === 'rental') {
                continue;
            }
            if ($lesson->status === 'canceled') {
                ++$totals['canceledLessons'];
                continue;
            }
            $counts = [
                'lessons' => 1, 'capacity' => $lesson->capacity, 'waitlistEntries' => $waitlist[$lesson->id] ?? 0,
            ] + ($bookings[$lesson->id] ?? []) + self::COUNTS;
            $totals = self::sum($totals, $counts);
            $date = (string) Date::fromInstant($lesson->startsAt, $zone);
            if (isset($periodByDate[$date])) {
                $period = $periodByDate[$date];
                $periodCounts[$period] = self::sum($periodCounts[$period], $counts);
            }

            $course = $lesson->courseType ?? throw new RuntimeException('Expected a course type for a bookable lesson');
            $teacher = $lesson->instructor ?? throw new RuntimeException('Expected an instructor for a bookable lesson');
            $labels = ['courseTypeName' => $course['name']];
            if ($lesson->classId === null) {
                $oneOffs[] = $labels + ['lessonId' => $lesson->id, 'startsAt' => ScheduleService::instant($lesson->startsAt)]
                    + self::withRates($counts);
            } else {
                self::addClass($classes, $lesson, $slots[$lesson->classId], $course['name'], $counts);
            }
            self::addGroup($rooms, $lesson->room['id'], [$lesson->location['name'], $lesson->room['name'], $lesson->room['id']], [
                'roomId' => $lesson->room['id'], 'name' => $lesson->room['name'], 'locationName' => $lesson->location['name'],
            ], $counts);
            self::addGroup($courseTypes, $course['id'], [$course['name'], $course['id']], [
                'courseTypeId' => $course['id'], 'name' => $course['name'],
            ], $counts);
            self::addGroup($instructors, $teacher['id'], [$teacher['displayName'], $teacher['id']], [
                'staffId' => $teacher['id'], 'name' => $teacher['displayName'],
            ], $counts);
        }

        foreach ($this->store->sales($from, $to, $currency) as $day) {
            $sales = ['passesSold' => $day['passesSold'], 'salesGross' => $day['salesGross']];
            $totals = self::sum($totals, $sales + ['eventFees' => $day['eventFees']]);
            $period = $periodByDate[$day['date']];
            $periodCounts[$period] = self::sum($periodCounts[$period], $sales);
        }
        $totals['activeMembers'] = $this->store->activeMembers($start, $end);

        return [
            'events' => $this->store->events($start, $end), 'totals' => self::withRates($totals),
            'periods' => array_map(static fn(array $span, array $counts): array => $span + self::withRates($counts), $periods, $periodCounts),
            'classes' => array_merge(self::finishGroups($classes), $oneOffs), 'rooms' => self::finishGroups($rooms),
            'courseTypes' => self::finishGroups($courseTypes), 'instructors' => self::finishGroups($instructors),
            'topParticipants' => $this->store->topParticipants($start, $end),
        ];
    }

    /** @return array{list<array{start:string,end:string}>, array<string,int>} */
    private static function periods(Date $from, Date $to, bool $monthly): array
    {
        $periods = [];
        $byDate = [];
        $start = $from;
        while (true) {
            $day = new DateTimeImmutable((string) $start, new DateTimeZone('UTC'));
            $daysLeft = $monthly ? (int) $day->format('t') - (int) $day->format('j') : 6 - self::mondayIndex($start->weekday());
            $offset = min($start->daysUntil($to), $daysLeft);
            $end = $start->addDays($offset);
            $index = count($periods);
            for ($i = 0; $i <= $offset; ++$i) {
                $byDate[(string) $start->addDays($i)] = $index;
            }
            $periods[] = ['start' => (string) $start, 'end' => (string) $end];
            if ($end->compare($to) === 0) {
                return [$periods, $byDate];
            }
            $start = $end->addDays(1);
        }
    }

    private static function mondayIndex(int $weekday): int
    {
        return ($weekday + 6) % 7;
    }

    /**
     * @param array<string,Group> $classes
     * @param ClassSlot $slot
     * @param array<string,int> $counts
     */
    private static function addClass(array &$classes, PublishedLesson $lesson, array $slot, string $courseName, array $counts): void
    {
        $id = $lesson->classId;
        assert($id !== null);
        self::addGroup($classes, $id, [self::mondayIndex($slot['weekday']), $slot['startTime'], $courseName, $id], [
            'classId' => $id, 'courseTypeName' => $courseName,
        ] + $slot + ScheduleService::audience($lesson), $counts);
    }

    /**
     * @param array<string,Group> $groups
     * @param list<int|string> $order
     * @param array<string,mixed> $labels
     * @param array<string,int> $counts
     */
    private static function addGroup(array &$groups, string $id, array $order, array $labels, array $counts): void
    {
        $sum = self::sum($groups[$id]['counts'] ?? self::COUNTS, $counts);
        $groups[$id] = ['order' => $order, 'labels' => $labels, 'counts' => $sum];
    }

    /**
     * @param array<string,Group> $groups
     * @return list<array<string,mixed>>
     */
    private static function finishGroups(array $groups): array
    {
        usort($groups, static function (array $left, array $right): int {
            foreach ($left['order'] as $i => $a) {
                $comparison = strcmp((string) $a, (string) $right['order'][$i]);
                if ($comparison !== 0) {
                    return $comparison;
                }
            }

            return 0;
        });

        return array_map(static fn(array $row): array => $row['labels'] + self::withRates($row['counts']), $groups);
    }

    /**
     * @param array<string,int> $into
     * @param array<string,int> $counts
     * @return array<string,int>
     */
    private static function sum(array $into, array $counts): array
    {
        foreach ($counts as $key => $value) {
            $into[$key] += $value;
        }

        return $into;
    }

    /**
     * @param array<string,int> $counts
     * @return array<string,int|float>
     */
    private static function withRates(array $counts): array
    {
        $taken = $counts['attended'] + $counts['noShows'];
        $counts['utilization'] = $counts['capacity'] === 0 ? 0 : $counts['bookings'] / $counts['capacity'];
        if ($taken !== 0) {
            $counts['noShowRate'] = $counts['noShows'] / $taken;
        }

        return $counts;
    }
}
