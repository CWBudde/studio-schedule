<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use StudioSchedule\Service\SystemClock;

final class SystemClockTest extends TestCase
{
    public function testSystemClockReportsTheCurrentInstantInUtc(): void
    {
        $previousTimezone = date_default_timezone_get();
        date_default_timezone_set('America/New_York');
        try {
            $clock = new SystemClock();
            self::assertInstanceOf(ClockInterface::class, $clock);
            $before = new DateTimeImmutable();
            $instant = $clock->now();
            $after = new DateTimeImmutable();

            self::assertGreaterThanOrEqual($before, $instant);
            self::assertLessThanOrEqual($after, $instant);
            self::assertSame('UTC', $instant->getTimezone()->getName());
        } finally {
            date_default_timezone_set($previousTimezone);
        }
    }
}
