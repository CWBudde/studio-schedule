<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use Generator;
use StudioSchedule\Domain\ICalendarDocument;
use StudioSchedule\Domain\ICalendarEvent;
use StudioSchedule\Domain\ICalendarStatus;

/**
 * Writes the RFC 5545 calendar format shared with internal/ical.
 *
 * @phpstan-type ZoneTransition array{ts: int, offset: int, isdst: bool, abbr: string}
 */
final class ICalendarWriter
{
    public const string CONTENT_TYPE = 'text/calendar; charset=utf-8';
    private const string DATE_TIME = 'Ymd\THis';
    private const string REFRESH_INTERVAL = 'PT1H';
    private const int MAX_LINE_OCTETS = 75;
    private const int DAY_SECONDS = 86400;
    private const int ZONE_LOOKBACK_SECONDS = 2 * 366 * self::DAY_SECONDS;

    /** @param list<ICalendarEvent> $events Events are written in the order given. */
    public function encode(string $name, DateTimeZone $zone, DateTimeImmutable $stamp, array $events): ICalendarDocument
    {
        $body = '';
        $hash = hash_init('sha256');
        foreach (self::calendarLines($name, $zone, $stamp, $events) as $property => $value) {
            $line = self::fold($property . ':' . $value);
            $body .= $line;
            if ($property !== 'DTSTAMP') {
                hash_update($hash, $line);
            }
        }

        return new ICalendarDocument($body, hash_final($hash));
    }

    /**
     * @param list<ICalendarEvent> $events
     *
     * @return Generator<string, string>
     */
    private static function calendarLines(string $name, DateTimeZone $zone, DateTimeImmutable $stamp, array $events): Generator
    {
        $escapedName = self::text($name);
        $tzid = $zone->getName();

        yield 'BEGIN' => 'VCALENDAR';
        yield 'VERSION' => '2.0';
        yield 'PRODID' => '-//studio-schedule//EN';
        yield 'CALSCALE' => 'GREGORIAN';
        yield 'METHOD' => 'PUBLISH';
        yield 'NAME' => $escapedName;
        yield 'X-WR-CALNAME' => $escapedName;
        yield 'X-WR-TIMEZONE' => self::text($tzid);
        yield 'REFRESH-INTERVAL;VALUE=DURATION' => self::REFRESH_INTERVAL;
        yield 'X-PUBLISHED-TTL' => self::REFRESH_INTERVAL;

        [$from, $to] = self::coveredYears($zone, $stamp, $events);
        yield from self::timezoneLines($zone, $from, $to);

        $stampValue = self::utcTime($stamp->getTimestamp());
        $tzidParameter = strpbrk($tzid, ';:,') === false ? $tzid : '"' . $tzid . '"';
        foreach ($events as $event) {
            yield 'BEGIN' => 'VEVENT';
            yield 'UID' => self::text($event->uid);
            yield 'DTSTAMP' => $stampValue;
            yield from self::dateTimeLine('DTSTART', $event->start, $zone, $tzidParameter);
            yield from self::dateTimeLine('DTEND', $event->end, $zone, $tzidParameter);
            yield 'SUMMARY' => self::text($event->summary);
            if ($event->location !== '') {
                yield 'LOCATION' => self::text($event->location);
            }
            if ($event->description !== '') {
                yield 'DESCRIPTION' => self::text($event->description);
            }
            if ($event->url !== '') {
                yield 'URL;VALUE=URI' => (string) preg_replace('/[\x00-\x1F\x7F]/', '', $event->url);
            }
            yield 'STATUS' => $event->status->value;
            yield 'SEQUENCE' => '0';
            yield 'TRANSP' => $event->status === ICalendarStatus::Cancelled ? 'TRANSPARENT' : 'OPAQUE';
            yield 'END' => 'VEVENT';
        }

        yield 'END' => 'VCALENDAR';
    }

    /**
     * The year boundaries in UTC, padded a day to cover every local offset.
     *
     * @param list<ICalendarEvent> $events
     *
     * @return array{int, int}
     */
    private static function coveredYears(DateTimeZone $zone, DateTimeImmutable $stamp, array $events): array
    {
        $first = $events[0]->start ?? $stamp;
        $last = $events[0]->end ?? $stamp;
        foreach ($events as $event) {
            $first = min($first, $event->start);
            $last = max($last, $event->end);
        }
        $firstYear = (int) $first->setTimezone($zone)->format('Y');
        $lastYear = (int) $last->setTimezone($zone)->format('Y');
        $utcMidnight = $stamp->setTimezone(new DateTimeZone('UTC'))->setTime(0, 0);

        return [
            $utcMidnight->setDate($firstYear, 1, 1)->getTimestamp() - self::DAY_SECONDS,
            $utcMidnight->setDate($lastYear + 1, 1, 1)->getTimestamp() + self::DAY_SECONDS,
        ];
    }

    /** @return Generator<string, string> */
    private static function timezoneLines(DateTimeZone $zone, int $from, int $to): Generator
    {
        yield 'BEGIN' => 'VTIMEZONE';
        yield 'TZID' => self::text($zone->getName());

        $observances = self::observances($zone, $from, $to);
        if ($observances === []) {
            $local = (new DateTimeImmutable('@' . $from))->setTimezone($zone);
            $offset = $local->getOffset();
            yield from self::observanceLines('19700101T000000', $offset, $offset, $local->format('T'), $local->format('I') === '1');
        }
        foreach ($observances as [$previous, $current]) {
            // DTSTART is the local wall time using the offset just before the transition.
            $onset = gmdate(self::DATE_TIME, $current['ts'] + $previous['offset']);
            yield from self::observanceLines($onset, $previous['offset'], $current['offset'], $current['abbr'], $current['isdst']);
        }

        yield 'END' => 'VTIMEZONE';
    }

    /** @return list<array{ZoneTransition, ZoneTransition}> */
    private static function observances(DateTimeZone $zone, int $from, int $to): array
    {
        // The first row returned by PHP is the state at the requested start.
        $transitions = $zone->getTransitions($from - self::ZONE_LOOKBACK_SECONDS, $to + 1) ?: [];
        $previous = array_shift($transitions);
        if ($previous === null) {
            return [];
        }

        $lastBefore = [];
        $within = [];
        foreach ($transitions as $current) {
            if ($previous['offset'] === $current['offset'] && $previous['abbr'] === $current['abbr'] && $previous['isdst'] === $current['isdst']) {
                continue;
            }
            if ($current['ts'] <= $from) {
                $lastBefore = [[$previous, $current]];
            } else {
                $within[] = [$previous, $current];
            }
            $previous = $current;
        }

        return [...$lastBefore, ...$within];
    }

    /** @return Generator<string, string> */
    private static function observanceLines(string $onset, int $fromOffset, int $toOffset, string $name, bool $daylight): Generator
    {
        $kind = $daylight ? 'DAYLIGHT' : 'STANDARD';
        yield 'BEGIN' => $kind;
        yield 'DTSTART' => $onset;
        yield 'TZOFFSETFROM' => self::formatOffset($fromOffset);
        yield 'TZOFFSETTO' => self::formatOffset($toOffset);
        if ($name !== '') {
            yield 'TZNAME' => self::text($name);
        }
        yield 'END' => $kind;
    }

    /** @return Generator<string, string> */
    private static function dateTimeLine(string $name, DateTimeImmutable $time, DateTimeZone $zone, string $tzid): Generator
    {
        $instant = $time->getTimestamp();
        $wallTime = $instant + $zone->getOffset($time);

        // A local time names the first occurrence. Use UTC for the later one when clocks go back.
        foreach ([-26 * 3600, 26 * 3600] as $delta) {
            $offset = $zone->getOffset($time->setTimestamp($instant + $delta));
            $candidate = $wallTime - $offset;
            if ($candidate < $instant && $zone->getOffset($time->setTimestamp($candidate)) === $offset) {
                yield $name => self::utcTime($instant);

                return;
            }
        }

        yield $name . ';TZID=' . $tzid => gmdate(self::DATE_TIME, $wallTime);
    }

    private static function utcTime(int $instant): string
    {
        return gmdate(self::DATE_TIME, $instant) . 'Z';
    }

    private static function formatOffset(int $offset): string
    {
        $seconds = abs($offset);
        $hoursAndMinutes = sprintf('%s%02d%02d', $offset < 0 ? '-' : '+', intdiv($seconds, 3600), intdiv($seconds, 60) % 60);

        return $seconds % 60 === 0 ? $hoursAndMinutes : $hoursAndMinutes . sprintf('%02d', $seconds % 60);
    }

    private static function text(string $value): string
    {
        $escaped = strtr($value, [
            '\\' => '\\\\', ';' => '\\;', ',' => '\\,', "\r\n" => '\\n', "\r" => '\\n', "\n" => '\\n',
        ]);

        return (string) preg_replace('/[\x00-\x08\x0A-\x1F\x7F]/', '', $escaped);
    }

    private static function fold(string $line): string
    {
        $folded = '';
        $limit = self::MAX_LINE_OCTETS;
        while (strlen($line) > $limit) {
            $cut = $limit;
            // UTF-8 continuation bytes have the prefix 10 and cannot start the next line.
            while ($cut > 0 && (ord($line[$cut]) & 0xC0) === 0x80) {
                --$cut;
            }
            $cut = $cut ?: $limit;
            $folded .= substr($line, 0, $cut) . "\r\n ";
            $line = substr($line, $cut);
            $limit = self::MAX_LINE_OCTETS - 1; // The leading space counts.
        }

        return $folded . $line . "\r\n";
    }
}
