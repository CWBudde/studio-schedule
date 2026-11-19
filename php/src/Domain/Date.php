<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

use DateTimeImmutable;
use DateTimeInterface;
use DateTimeZone;
use InvalidArgumentException;
use JsonSerializable;
use LogicException;
use Stringable;

/** A school calendar date with no associated time zone. */
final readonly class Date implements JsonSerializable, Stringable
{
    private const int SECONDS_PER_DAY = 86400;
    // Day numbers measured from 1970-01-01.
    private const int FIRST_DAY = -719162; // 0001-01-01
    private const int LAST_DAY = 2932896; // 9999-12-31

    private function __construct(private ?DateTimeImmutable $midnightUtc) {}

    public static function parse(string $value): self
    {
        if (preg_match('/\A([0-9]{4})-([0-9]{2})-([0-9]{2})\z/', $value, $parts) !== 1
            || !checkdate((int) $parts[2], (int) $parts[3], (int) $parts[1])
        ) {
            throw new InvalidArgumentException('Expected an existing calendar date in YYYY-MM-DD form.');
        }

        return new self(new DateTimeImmutable($value, new DateTimeZone('UTC')));
    }

    /** The text and JSON representation uses an empty string for an absent date. */
    public static function fromText(string $value): self
    {
        return $value === '' ? new self(null) : self::parse($value);
    }

    public static function fromInstant(DateTimeInterface $instant, DateTimeZone $schoolTimeZone): self
    {
        $local = DateTimeImmutable::createFromInterface($instant)->setTimezone($schoolTimeZone);

        return self::parse($local->format('Y-m-d'));
    }

    public function isZero(): bool
    {
        return $this->midnightUtc === null;
    }

    public function compare(self $other): int
    {
        return (string) $this <=> (string) $other;
    }

    public function addDays(int $days): self
    {
        $midnight = $this->utc();
        $day = intdiv($midnight->getTimestamp(), self::SECONDS_PER_DAY);
        if ($days < self::FIRST_DAY - $day || $days > self::LAST_DAY - $day) {
            throw new InvalidArgumentException('Calendar date falls outside years 0001–9999.');
        }

        return new self($midnight->setTimestamp(($day + $days) * self::SECONDS_PER_DAY));
    }

    public function daysUntil(self $other): int
    {
        return intdiv($other->utc()->getTimestamp() - $this->utc()->getTimestamp(), self::SECONDS_PER_DAY);
    }

    /** Sunday is 0 and Saturday is 6, as in Go. */
    public function weekday(): int
    {
        return (int) $this->utc()->format('w');
    }

    /**
     * Returns the UTC instant at which the school's clock shows the given time.
     * Repeated times resolve to the earlier instant. Skipped times move forward by the gap.
     */
    public function at(TimeOfDay $time, DateTimeZone $schoolTimeZone): DateTimeImmutable
    {
        return self::wallClockInstant($this->utc()->setTime($time->hour, $time->minute), $schoolTimeZone);
    }

    /** The next school midnight can fit in UTC even when the local date is outside the Date range. */
    public function nextMidnight(DateTimeZone $schoolTimeZone): DateTimeImmutable
    {
        return self::wallClockInstant($this->utc()->modify('+1 day'), $schoolTimeZone);
    }

    private static function wallClockInstant(DateTimeImmutable $wall, DateTimeZone $schoolTimeZone): DateTimeImmutable
    {
        $seconds = $wall->getTimestamp();

        // A full day on each side covers both offsets at a transition, even near the date line.
        $before = $schoolTimeZone->getOffset($wall->setTimestamp($seconds - self::SECONDS_PER_DAY));
        $after = $schoolTimeZone->getOffset($wall->setTimestamp($seconds + self::SECONDS_PER_DAY));
        $requested = $wall->format('Y-m-d H:i');
        $earliest = null;
        foreach ([$before, $after] as $offset) {
            $candidate = $wall->setTimestamp($seconds - $offset);
            if ($candidate->setTimezone($schoolTimeZone)->format('Y-m-d H:i') === $requested
                && ($earliest === null || $candidate < $earliest)
            ) {
                $earliest = $candidate;
            }
        }

        // Applying the offset before the gap places the time after it by the size of the shift.
        return $earliest ?? $wall->setTimestamp($seconds - $before);
    }

    public function __toString(): string
    {
        return $this->midnightUtc?->format('Y-m-d') ?? '';
    }

    public function jsonSerialize(): string
    {
        return (string) $this;
    }

    private function utc(): DateTimeImmutable
    {
        return $this->midnightUtc ?? throw new LogicException('This operation requires a calendar date.');
    }
}
