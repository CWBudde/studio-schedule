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
        public int $waitlistInviteExpiryMicros,
    ) {}

    /** @return array{lastReleaseAt: DateTimeImmutable, nextReleaseAt: DateTimeImmutable, releasedUntil: DateTimeImmutable, ruleReleasedUntil: DateTimeImmutable} */
    public function releaseWindow(DateTimeImmutable $now): array
    {
        $today = Date::fromInstant($now, $this->timezone);
        $day = $today->addDays(-(($today->weekday() - $this->releaseWeekday + 7) % 7));
        $last = $day->at($this->releaseTime, $this->timezone);
        if ($last > $now) {
            $day = $day->addDays(-7);
            $last = $day->at($this->releaseTime, $this->timezone);
        }

        $rule = $this->midnight($day->addDays(7 * $this->releaseWeeksAhead + 1));
        $until = $rule;
        if ($this->releasedThrough !== null) {
            $until = max($until, $this->midnight($this->releasedThrough->addDays(1)));
        }

        return [
            'lastReleaseAt' => $last,
            'nextReleaseAt' => $day->addDays(7)->at($this->releaseTime, $this->timezone),
            'releasedUntil' => $until,
            'ruleReleasedUntil' => $rule,
        ];
    }

    public function withReleaseRule(int $weekday, TimeOfDay $time, int $weeksAhead, ?Date $through): self
    {
        return new self(
            $this->timezone,
            $weekday,
            $time,
            $weeksAhead,
            $through,
            $this->defaultVisibleDays,
            $this->bookingCutoffMicros,
            $this->cancellationCutoffMicros,
            $this->waitlistInviteExpiryMicros,
        );
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
