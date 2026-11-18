<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

use InvalidArgumentException;
use JsonSerializable;
use Stringable;

/** A wall-clock time in the school's time zone, with minute precision. */
final readonly class TimeOfDay implements JsonSerializable, Stringable
{
    private function __construct(public int $hour, public int $minute) {}

    public static function parse(string $value): self
    {
        if (preg_match('/\A([01][0-9]|2[0-3]):([0-5][0-9])\z/', $value, $parts) !== 1) {
            throw new InvalidArgumentException('Expected a wall-clock time in HH:MM form.');
        }

        return new self((int) $parts[1], (int) $parts[2]);
    }

    public function minutesSinceMidnight(): int
    {
        return $this->hour * 60 + $this->minute;
    }

    public function compare(self $other): int
    {
        return $this->minutesSinceMidnight() <=> $other->minutesSinceMidnight();
    }

    public function __toString(): string
    {
        return sprintf('%02d:%02d', $this->hour, $this->minute);
    }

    public function jsonSerialize(): string
    {
        return (string) $this;
    }
}
