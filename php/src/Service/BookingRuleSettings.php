<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use Psr\Log\LoggerInterface;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Domain\ScheduleRules;
use StudioSchedule\Domain\TimeOfDay;
use Throwable;

final readonly class BookingRuleSettings
{
    private const int MICROS_PER_MINUTE = 60_000_000;
    private const int MAX_RELEASED_THROUGH_DAYS = 52 * 7;

    public function __construct(
        private SchedulePersistence $store,
        private ClockInterface $clock,
        private LessonGenerator $generator,
        private LoggerInterface $logger,
    ) {}

    /** @return array<string, int|string|array<string, string>> */
    public function read(): array
    {
        $rules = $this->store->inReadSnapshot($this->store->rules(...));

        return self::view($rules, $this->clock->now(), 0);
    }

    /** @return array<string, int|string|array<string, string>> */
    public function update(
        int $weekday,
        TimeOfDay $time,
        int $weeksAhead,
        ?Date $through,
        int $bookingMinutes,
        int $cancellationMinutes,
        int $inviteMinutes,
        int $visibleDays,
    ): array {
        $widened = false;
        $rules = $this->store->changeBookingRules(function (ScheduleRules $current) use (
            $weekday,
            $time,
            $weeksAhead,
            $through,
            $bookingMinutes,
            $cancellationMinutes,
            $inviteMinutes,
            $visibleDays,
            &$widened,
        ): ScheduleRules {
            $today = Date::fromInstant($this->clock->now(), $current->timezone);
            self::validateThrough($through, $today);
            $throughWider = $through !== null
                && ($current->releasedThrough === null || $through->compare($current->releasedThrough) > 0)
                && $today->daysUntil($through) > 7 * ($weeksAhead + 2);
            $widened = $weeksAhead > $current->releaseWeeksAhead || $visibleDays > $current->defaultVisibleDays || $throughWider;

            return new ScheduleRules(
                $current->timezone,
                $weekday,
                $time,
                $weeksAhead,
                $through,
                $visibleDays,
                $bookingMinutes * self::MICROS_PER_MINUTE,
                $cancellationMinutes * self::MICROS_PER_MINUTE,
                $inviteMinutes * self::MICROS_PER_MINUTE,
            );
        });

        $now = $this->clock->now();
        $generated = 0;
        if ($widened) {
            try {
                $generated = $this->generator->generateAhead();
            } catch (Throwable $error) {
                // The rules are already committed and the hourly job will retry generation.
                $this->logger->warning('generate the lessons of a wider release window', ['exception' => $error::class]);
            }
        }

        return self::view($rules, $now, $generated);
    }

    /** @return array<string, int|string|array<string, string>> */
    private static function view(ScheduleRules $rules, DateTimeImmutable $now, int $generated): array
    {
        $body = [
            'releaseWeekday' => $rules->releaseWeekday,
            'releaseTime' => (string) $rules->releaseTime,
            'releaseWeeksAhead' => $rules->releaseWeeksAhead,
            'bookingCutoffMinutes' => intdiv($rules->bookingCutoffMicros, self::MICROS_PER_MINUTE),
            'cancellationCutoffMinutes' => intdiv($rules->cancellationCutoffMicros, self::MICROS_PER_MINUTE),
            'waitlistInviteExpiryMinutes' => intdiv($rules->waitlistInviteExpiryMicros, self::MICROS_PER_MINUTE),
            'defaultVisibleDays' => $rules->defaultVisibleDays,
            'window' => self::window($rules, $now),
            'generatedLessons' => $generated,
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
        self::validateThrough($through, $today);

        return self::window($rules->withReleaseRule($weekday, $time, $weeksAhead, $through), $now);
    }

    private static function validateThrough(?Date $through, Date $today): void
    {
        if ($through !== null && $today->daysUntil($through) > self::MAX_RELEASED_THROUGH_DAYS) {
            throw new AdminSettingsFieldError('releasedThrough', 'released-through date must be at most 52 weeks after today');
        }
    }

    /** @return array<string, string> */
    private static function window(ScheduleRules $rules, DateTimeImmutable $now): array
    {
        return ['now' => Instant::format($now), ...array_map(Instant::format(...), $rules->releaseWindow($now))];
    }
}
