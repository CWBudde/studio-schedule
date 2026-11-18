<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use DateTimeZone;
use InvalidArgumentException;
use LogicException;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Money;
use StudioSchedule\Domain\TimeOfDay;

final class DomainValuesTest extends TestCase
{
    public function testValuesUseTheGoJsonFormats(): void
    {
        $money = new Money(-9007199254740993);
        $date = Date::parse('2028-02-29');
        $time = TimeOfDay::parse('07:05');

        self::assertSame(-9007199254740993, $money->minorUnits);
        self::assertSame('2028-02-29', (string) $date);
        self::assertSame('07:05', (string) $time);
        self::assertSame(
            '[-9007199254740993,"2028-02-29","07:05"]',
            json_encode([$money, $date, $time], JSON_THROW_ON_ERROR),
        );
    }

    public function testEmptyDateTextRepresentsAnAbsentDate(): void
    {
        $empty = Date::fromText('');

        self::assertTrue($empty->isZero());
        self::assertSame('""', json_encode($empty, JSON_THROW_ON_ERROR));
        self::assertSame(-1, $empty->compare(Date::parse('0001-01-01')));
        self::assertFalse(Date::fromText('0001-01-01')->isZero());
    }

    public function testAnAbsentDateCannotBecomeAnInstant(): void
    {
        $this->expectException(LogicException::class);
        Date::fromText('')->at(TimeOfDay::parse('00:00'), new DateTimeZone('UTC'));
    }

    #[DataProvider('invalidDates')]
    public function testDateParsingRejectsInvalidAndNoncanonicalValues(string $value): void
    {
        $this->expectException(InvalidArgumentException::class);
        Date::parse($value);
    }

    /** @return iterable<string, array{string}> */
    public static function invalidDates(): iterable
    {
        yield 'missing' => [''];
        yield 'short month' => ['2026-2-28'];
        yield 'zero year' => ['0000-01-01'];
        yield 'five digit year' => ['10000-01-01'];
        yield 'impossible date' => ['2026-02-29'];
        yield 'non-leap century' => ['1900-02-29'];
        yield 'trailing newline' => ["2026-02-28\n"];
        yield 'instant' => ['2026-02-28T00:00:00Z'];
    }

    #[DataProvider('invalidTimes')]
    public function testTimeParsingRejectsValuesOutsideTheMinutePrecisionWallClock(string $value): void
    {
        $this->expectException(InvalidArgumentException::class);
        TimeOfDay::parse($value);
    }

    /** @return iterable<string, array{string}> */
    public static function invalidTimes(): iterable
    {
        yield 'missing' => [''];
        yield 'short hour' => ['7:05'];
        yield 'short minute' => ['07:5'];
        yield 'hour overflow' => ['24:00'];
        yield 'minute overflow' => ['12:60'];
        yield 'seconds' => ['12:00:00'];
        yield 'time offset' => ['12:00+01:00'];
        yield 'trailing newline' => ["12:00\n"];
    }

    public function testTimeOrderingAndMinutesSinceMidnight(): void
    {
        $morning = TimeOfDay::parse('09:59');
        $later = TimeOfDay::parse('10:00');

        self::assertSame(-1, $morning->compare($later));
        self::assertSame(1, $later->compare($morning));
        self::assertSame(0, $later->compare(TimeOfDay::parse('10:00')));
        self::assertSame(599, $morning->minutesSinceMidnight());
        self::assertSame(0, TimeOfDay::parse('00:00')->minutesSinceMidnight());
        self::assertSame(1439, TimeOfDay::parse('23:59')->minutesSinceMidnight());
    }

    #[DataProvider('calendarRanges')]
    public function testCalendarArithmetic(string $start, string $end, int $days): void
    {
        $first = Date::parse($start);
        $last = Date::parse($end);

        self::assertSame($days, $first->daysUntil($last));
        self::assertSame(-$days, $last->daysUntil($first));
        self::assertSame($end, (string) $first->addDays($days));
        self::assertSame($start, (string) $last->addDays(-$days));
    }

    /** @return iterable<string, array{string, string, int}> */
    public static function calendarRanges(): iterable
    {
        yield 'spring DST' => ['2026-03-28', '2026-03-30', 2];
        yield 'autumn DST' => ['2026-10-22', '2026-10-29', 7];
        yield 'same day' => ['2026-10-23', '2026-10-23', 0];
        yield 'leap century' => ['2000-02-28', '2000-03-01', 2];
        yield 'year boundary' => ['2026-12-31', '2027-01-01', 1];
        yield 'supported range' => ['0001-01-01', '9999-12-31', 3652058];
    }

    #[DataProvider('outOfRangeAdditions')]
    public function testCalendarAdditionRejectsResultsOutsideTheSupportedYears(string $date, int $days): void
    {
        $this->expectException(InvalidArgumentException::class);
        Date::parse($date)->addDays($days);
    }

    /** @return iterable<string, array{string, int}> */
    public static function outOfRangeAdditions(): iterable
    {
        yield 'below lower boundary' => ['0001-01-01', -1];
        yield 'above upper boundary' => ['9999-12-31', 1];
        yield 'integer overflow' => ['2026-10-23', PHP_INT_MAX];
    }

    public function testDateOrderingAndWeekdayFollowTheGoCalendar(): void
    {
        $sunday = Date::parse('2026-10-25');
        $monday = Date::parse('2026-10-26');

        self::assertSame(-1, $sunday->compare($monday));
        self::assertSame(0, $monday->compare(Date::parse('2026-10-26')));
        self::assertSame(1, $monday->compare($sunday));
        self::assertSame(0, $sunday->weekday());
        self::assertSame(1, $monday->weekday());
        self::assertSame(6, Date::parse('2026-10-24')->weekday());
    }

    public function testTheSameInstantHasDifferentDatesInDifferentSchoolTimeZones(): void
    {
        $instant = new DateTimeImmutable('2026-10-24T22:30:00Z');

        self::assertSame('2026-10-25', (string) Date::fromInstant($instant, new DateTimeZone('Europe/Berlin')));
        self::assertSame('2026-10-24', (string) Date::fromInstant($instant, new DateTimeZone('America/New_York')));
    }

    #[DataProvider('schoolWallTimes')]
    public function testSchoolWallTimeResolvesToTheExpectedUtcInstant(
        string $zone,
        string $date,
        string $time,
        string $expected,
    ): void {
        $instant = Date::parse($date)->at(TimeOfDay::parse($time), new DateTimeZone($zone));
        self::assertSame($expected, $instant->format('Y-m-d\TH:i:sP'));
    }

    /** @return iterable<string, array{string, string, string, string}> */
    public static function schoolWallTimes(): iterable
    {
        yield 'Berlin fall-back evening' => ['Europe/Berlin', '2026-10-25', '18:00', '2026-10-25T17:00:00+00:00'];
        yield 'Berlin fold' => ['Europe/Berlin', '2026-10-25', '02:30', '2026-10-25T00:30:00+00:00'];
        yield 'Berlin gap' => ['Europe/Berlin', '2027-03-28', '02:30', '2027-03-28T01:30:00+00:00'];
        yield 'Auckland fold' => ['Pacific/Auckland', '2026-04-05', '02:30', '2026-04-04T13:30:00+00:00'];
        yield 'Auckland gap' => ['Pacific/Auckland', '2026-09-27', '02:30', '2026-09-26T14:30:00+00:00'];
        yield 'Lord Howe half-hour fold' => ['Australia/Lord_Howe', '2026-04-05', '01:45', '2026-04-04T14:45:00+00:00'];
        yield 'Lord Howe half-hour gap' => ['Australia/Lord_Howe', '2026-10-04', '02:15', '2026-10-03T15:45:00+00:00'];
        yield 'Apia skipped date' => ['Pacific/Apia', '2011-12-30', '12:00', '2011-12-30T22:00:00+00:00'];
        yield 'UTC' => ['UTC', '2026-10-25', '02:30', '2026-10-25T02:30:00+00:00'];
        yield 'fixed fractional offset' => ['+05:45', '2026-10-25', '02:30', '2026-10-24T20:45:00+00:00'];
    }
}
