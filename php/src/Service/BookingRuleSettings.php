<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Domain\ScheduleRules;
use StudioSchedule\Domain\TimeOfDay;

final readonly class BookingRuleSettings
{
    private const int MICROS_PER_MINUTE = 60_000_000;
    private const int MAX_RELEASED_THROUGH_DAYS = 52 * 7;

    public function __construct(private SchedulePersistence $store, private ClockInterface $clock) {}

    /** @return array<string, int|string|array<string, string>> */
    public function read(): array
    {
        $rules = $this->store->inReadSnapshot($this->store->rules(...));
        $body = [
            'releaseWeekday' => $rules->releaseWeekday,
            'releaseTime' => (string) $rules->releaseTime,
            'releaseWeeksAhead' => $rules->releaseWeeksAhead,
            'bookingCutoffMinutes' => intdiv($rules->bookingCutoffMicros, self::MICROS_PER_MINUTE),
            'cancellationCutoffMinutes' => intdiv($rules->cancellationCutoffMicros, self::MICROS_PER_MINUTE),
            'waitlistInviteExpiryMinutes' => intdiv($rules->waitlistInviteExpiryMicros, self::MICROS_PER_MINUTE),
            'defaultVisibleDays' => $rules->defaultVisibleDays,
            'window' => self::window($rules, $this->clock->now()),
            'generatedLessons' => 0,
        ];
        if ($rules->releasedThrough !== null) {
            $body['releasedThrough'] = (string) $rules->releasedThrough;
        }

        return $body;
    }

    /** @return array<string, string> */
    public function preview(int $weekday, TimeOfDay $time, int $weeksAhead, ?Date $through): array
    {
        $rules = $this->store->inReadSnapshot($this->store->rules(...));
        $now = $this->clock->now();
        $today = Date::fromInstant($now, $rules->timezone);
        if ($through !== null && $today->daysUntil($through) > self::MAX_RELEASED_THROUGH_DAYS) {
            throw new AdminSettingsFieldError('releasedThrough', 'released-through date must be at most 52 weeks after today');
        }

        return self::window($rules->withReleaseRule($weekday, $time, $weeksAhead, $through), $now);
    }

    /** @return array<string, string> */
    private static function window(ScheduleRules $rules, DateTimeImmutable $now): array
    {
        return ['now' => Instant::format($now), ...array_map(Instant::format(...), $rules->releaseWindow($now))];
    }
}
