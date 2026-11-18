<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

use DateTimeImmutable;
use DateTimeZone;

final readonly class ScheduleRules
{
    public function __construct(
        public DateTimeZone $timezone,
        public int $releaseWeekday,
        public TimeOfDay $releaseTime,
        public int $releaseWeeksAhead,
        public ?Date $releasedThrough,
        public int $defaultVisibleDays,
        public int $bookingCutoffMicros,
        public int $cancellationCutoffMicros,
    ) {}

    /** @return array{releasedUntil: DateTimeImmutable, nextReleaseAt: DateTimeImmutable} */
    public function releaseWindow(DateTimeImmutable $now): array
    {
        $today = Date::fromInstant($now, $this->timezone);
        $day = $today->addDays(-(($today->weekday() - $this->releaseWeekday + 7) % 7));
        if ($day->at($this->releaseTime, $this->timezone) > $now) {
            $day = $day->addDays(-7);
        }

        $until = $this->midnight($day->addDays(7 * $this->releaseWeeksAhead + 1));
        if ($this->releasedThrough !== null) {
            $until = max($until, $this->midnight($this->releasedThrough->addDays(1)));
        }

        return [
            'releasedUntil' => $until,
            'nextReleaseAt' => $day->addDays(7)->at($this->releaseTime, $this->timezone),
        ];
    }

    public function midnight(Date $date): DateTimeImmutable
    {
        return $date->at(TimeOfDay::parse('00:00'), $this->timezone);
    }

    public function earlyUntil(DateTimeImmutable $now, ?int $visibleDays): DateTimeImmutable
    {
        $days = $visibleDays !== null && $visibleDays > 0 ? $visibleDays : $this->defaultVisibleDays;
        $today = Date::fromInstant($now, $this->timezone);

        return $this->midnight($today->addDays($days + 1));
    }
}
