<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use DateTimeZone;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Domain\ICalendarEvent;
use StudioSchedule\Domain\ICalendarStatus;
use StudioSchedule\Service\ICalendarWriter;

final class ICalendarWriterTest extends TestCase
{
    private const string BERLIN = 'Europe/Berlin';
    private const string SCHOOL = 'Studio Example – ';
    private const string STAMP = '2026-10-21T10:00:00Z';
    private const string ROOM = 'Studio Nowhere, Room One';
    private const string OTHER_ROOM = 'Studio Elsewhere, Room Two';
    private const string ADDRESS = ', 1 Example Road, 12345 Sampletown';

    /** @return iterable<string, array{string, string, string, list<ICalendarEvent>}> */
    public static function goFeeds(): iterable
    {
        // Recreate the writer inputs from TestCalendarFeed* and TestEventRegistrationFeed in internal/service.
        yield 'bookings' => [
            self::SCHOOL . 'Meine Stunden', self::BERLIN, self::STAMP,
            [
                self::booking('17', '2026-09-30T16:00:00Z', 'Mia Example'),
                self::booking('18', '2026-10-22T16:00:00Z', 'Anna Example, Mia Example', accompanist: true),
                self::booking('19', '2026-10-29T17:00:00Z', 'Tom Example', kind: 'Workshop', elsewhere: true),
                self::booking('1a', '2026-10-30T17:00:00Z', 'Anna Example', canceled: 'Instructor ill; sorry, no replacement'),
                self::booking('1b', '2026-12-10T17:00:00Z', 'Mia Example', kind: 'Privatstunde'),
            ],
        ];
        yield 'registered-sessions' => [
            self::SCHOOL . 'Meine Stunden', self::BERLIN, self::STAMP,
            [
                self::booking('1e', '2026-10-23T16:00:00Z', 'Anna Example, Mia Example', kind: 'Workshop', duration: 75),
                self::booking(
                    '1f',
                    '2026-10-24T16:00:00Z',
                    'Anna Example, Mia Example',
                    kind: 'Workshop',
                    canceled: 'Diese Stunde fällt aus.',
                    duration: 75,
                ),
            ],
        ];
        yield 'schedule' => [
            self::SCHOOL . 'Schedule', self::BERLIN, self::STAMP,
            [
                self::schedule('18', '2026-10-22T16:00:00Z'),
                self::schedule('19', '2026-10-23T16:00:00Z', elsewhere: true),
                self::schedule('1b', '2026-10-28T17:00:00Z', canceled: true),
                self::schedule('1c', '2026-11-04T17:00:00Z', elsewhere: true),
                self::schedule('1e', '2026-11-14T09:00:00Z', workshop: true),
            ],
        ];
        yield 'schedule-location' => [
            self::SCHOOL . 'Schedule (Studio Elsewhere)', self::BERLIN, self::STAMP,
            [
                self::schedule('19', '2026-10-23T16:00:00Z', elsewhere: true, locationFeed: true),
                self::schedule('1c', '2026-11-04T17:00:00Z', elsewhere: true, locationFeed: true),
            ],
        ];
        yield 'schedule-new-york' => [
            self::SCHOOL . 'Schedule', 'America/New_York', self::STAMP,
            [
                self::schedule('0a', '2026-10-29T22:00:00Z'),
                self::schedule('0b', '2026-11-05T23:00:00Z'),
            ],
        ];
        yield 'teaching' => [
            self::SCHOOL . 'Teaching', self::BERLIN, self::STAMP,
            [
                self::teaching('16', '2026-10-22T16:00:00Z', accompanist: 'Ben Example'),
                self::teaching('17', '2026-10-26T17:00:00Z', instructor: 'Ben Example', accompanist: 'Ada Example'),
                self::teaching('18', '2026-10-27T17:00:00Z', canceled: true),
                self::teaching('19', '2026-11-18T17:00:00Z'),
            ],
        ];
        yield 'teaching-spring' => [
            self::SCHOOL . 'Teaching', self::BERLIN, '2027-03-20T10:00:00Z',
            [
                self::teaching('16', '2027-03-25T17:00:00Z'),
                self::teaching('17', '2027-04-01T16:00:00Z'),
            ],
        ];
    }

    /** @param list<ICalendarEvent> $events */
    #[DataProvider('goFeeds')]
    public function testMatchesGoFeedByteForByte(string $name, string $zone, string $stamp, array $events): void
    {
        $directory = getenv('STUDIO_PHP_TEST_CALENDAR_FEEDS') ?: dirname(__DIR__, 2) . '/internal/service/testdata/feeds';
        $fixture = file_get_contents($directory . '/' . $this->dataName() . '.ics');
        self::assertIsString($fixture);
        $expected = str_replace("\n", "\r\n", $fixture);

        $writer = new ICalendarWriter();
        $document = $writer->encode($name, new DateTimeZone($zone), new DateTimeImmutable($stamp), $events);

        self::assertSame($expected, $document->body);
        self::assertSame(hash('sha256', (string) preg_replace('/^DTSTAMP:[^\r]+\r\n/m', '', $expected)), $document->fingerprint);
    }

    public function testFoldsAtOctetBoundariesWithoutSplittingUtf8(): void
    {
        $now = new DateTimeImmutable(self::STAMP);
        $event = new ICalendarEvent(
            uid: 'long@example.org',
            start: $now,
            end: $now->modify('+1 hour'),
            summary: str_repeat('a', 65) . '😀' . str_repeat('ü', 35) . 'ä',
            location: str_repeat('a', 66),
        );

        $document = (new ICalendarWriter())->encode('Test', new DateTimeZone('UTC'), $now, [$event]);

        self::assertStringContainsString("SUMMARY:" . str_repeat('a', 65) . "\r\n 😀" . str_repeat('ü', 35) . "\r\n ä\r\n", $document->body);
        self::assertStringContainsString("LOCATION:" . str_repeat('a', 66) . "\r\nSTATUS:", $document->body);
    }

    public function testEscapesTextAndStripsUriControlCharacters(): void
    {
        $now = new DateTimeImmutable(self::STAMP);
        $event = new ICalendarEvent(
            uid: 'escaped@example.org',
            start: $now,
            end: $now->modify('+1 hour'),
            summary: "Back\\slash; comma,",
            description: "Line 1\r\nline 2\rline 3\n\tline\0\x07\x7f 4",
            url: "https://school.example/\r\n\t\x7fschedule?lesson=1,2",
        );

        $document = (new ICalendarWriter())->encode('School; name', new DateTimeZone('UTC'), $now, [$event]);

        self::assertStringContainsString("NAME:School\\; name\r\nX-WR-CALNAME:School\\; name\r\n", $document->body);
        self::assertStringContainsString(
            "SUMMARY:Back\\\\slash\\; comma\\,\r\n"
            . "DESCRIPTION:Line 1\\nline 2\\nline 3\\n\tline 4\r\n"
            . "URL;VALUE=URI:https://school.example/schedule?lesson=1,2\r\n",
            $document->body,
        );
    }

    public function testOnlyTheSecondOccurrenceOfARepeatedWallTimeUsesUtc(): void
    {
        $event = new ICalendarEvent(
            uid: 'repeated@example.org',
            start: new DateTimeImmutable('2026-10-25T00:30:00.900Z'),
            end: new DateTimeImmutable('2026-10-25T01:30:00.900Z'),
            summary: 'Repeated hour',
            status: ICalendarStatus::Cancelled,
        );
        $document = (new ICalendarWriter())->encode('Test', new DateTimeZone(self::BERLIN), $event->start, [$event]);

        self::assertStringContainsString(
            "DTSTAMP:20261025T003000Z\r\nDTSTART;TZID=Europe/Berlin:20261025T023000\r\n"
            . "DTEND:20261025T013000Z\r\nSUMMARY:Repeated hour\r\n"
            . "STATUS:CANCELLED\r\nSEQUENCE:0\r\nTRANSP:TRANSPARENT\r\nEND:VEVENT\r\n",
            $document->body,
        );
    }

    public function testFingerprintIgnoresDocumentStampsAndTracksContentChanges(): void
    {
        $writer = new ICalendarWriter();
        $now = new DateTimeImmutable(self::STAMP);
        $zone = new DateTimeZone(self::BERLIN);
        $events = [self::schedule('18', '2026-10-22T16:00:00Z')];

        $original = $writer->encode('Original', $zone, $now, $events);
        $later = $writer->encode('Original', $zone, $now->modify('+2 years'), $events);
        $renamed = $writer->encode('Renamed', $zone, $now, $events);

        self::assertStringContainsString("DTSTAMP:20281021T100000Z\r\n", $later->body);
        self::assertSame($original->fingerprint, $later->fingerprint);
        self::assertNotSame($original->fingerprint, $renamed->fingerprint);
    }

    public function testEmptyFeedUsesStampYearInCalendarZone(): void
    {
        $stamp = new DateTimeImmutable('2026-12-31T14:00:00Z'); // Jan 1, 2027 in Sydney.
        $document = (new ICalendarWriter())->encode('Empty', new DateTimeZone('Australia/Sydney'), $stamp, []);

        self::assertStringContainsString(
            "BEGIN:VTIMEZONE\r\nTZID:Australia/Sydney\r\n"
            . "BEGIN:DAYLIGHT\r\nDTSTART:20261004T020000\r\nTZOFFSETFROM:+1000\r\nTZOFFSETTO:+1100\r\nTZNAME:AEDT\r\nEND:DAYLIGHT\r\n"
            . "BEGIN:STANDARD\r\nDTSTART:20270404T030000\r\nTZOFFSETFROM:+1100\r\nTZOFFSETTO:+1000\r\nTZNAME:AEST\r\nEND:STANDARD\r\n"
            . "BEGIN:DAYLIGHT\r\nDTSTART:20271003T020000\r\nTZOFFSETFROM:+1000\r\nTZOFFSETTO:+1100\r\nTZNAME:AEDT\r\nEND:DAYLIGHT\r\n"
            . "END:VTIMEZONE\r\nEND:VCALENDAR\r\n",
            $document->body,
        );
    }

    public function testTimezoneCoversBothYearsWhenAnEventCrossesTheLocalYearBoundary(): void
    {
        $event = new ICalendarEvent(
            uid: 'overnight@example.org',
            start: new DateTimeImmutable('2026-12-31T22:30:00Z'),
            end: new DateTimeImmutable('2026-12-31T23:30:00Z'),
            summary: 'Overnight',
        );
        $document = (new ICalendarWriter())->encode('Test', new DateTimeZone(self::BERLIN), $event->start, [$event]);

        self::assertStringContainsString("DTSTART:20251026T030000\r\n", $document->body);
        self::assertStringContainsString("DTSTART:20260329T020000\r\n", $document->body);
        self::assertStringContainsString("DTSTART:20261025T030000\r\n", $document->body);
        self::assertStringContainsString("DTSTART:20270328T020000\r\n", $document->body);
        self::assertStringContainsString("DTSTART:20271031T030000\r\n", $document->body);
        self::assertStringContainsString("DTSTART;TZID=Europe/Berlin:20261231T233000\r\nDTEND;TZID=Europe/Berlin:20270101T003000\r\n", $document->body);
    }

    public function testZoneWithoutNearbyTransitionsKeepsItsHistoricalSecondOffset(): void
    {
        $stamp = new DateTimeImmutable('1960-01-01T00:00:00Z');
        $document = (new ICalendarWriter())->encode('Historical', new DateTimeZone('Africa/Monrovia'), $stamp, []);

        self::assertStringContainsString(
            "BEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nTZOFFSETFROM:-004430\r\nTZOFFSETTO:-004430\r\nTZNAME:MMT\r\nEND:STANDARD\r\n",
            $document->body,
        );
    }

    public function testQuotesFixedOffsetTimeZoneParameters(): void
    {
        $stamp = new DateTimeImmutable(self::STAMP);
        $event = new ICalendarEvent('fixed@example.org', $stamp, $stamp->modify('+1 hour'), 'Fixed');
        $document = (new ICalendarWriter())->encode('Test', new DateTimeZone('+05:45'), $stamp, [$event]);

        self::assertStringContainsString("TZID:+05:45\r\nBEGIN:STANDARD\r\nDTSTART:19700101T000000\r\nTZOFFSETFROM:+0545\r\nTZOFFSETTO:+0545\r\n", $document->body);
        self::assertStringContainsString("DTSTART;TZID=\"+05:45\":20261021T154500\r\n", $document->body);
    }

    private static function booking(
        string $id,
        string $instant,
        string $people,
        string $kind = '',
        string $canceled = '',
        bool $accompanist = false,
        bool $elsewhere = false,
        int $duration = 60,
    ): ICalendarEvent {
        $room = $elsewhere ? self::OTHER_ROOM : self::ROOM;
        $description = ($canceled === '' ? '' : $canceled . "\n")
            . ($kind === '' ? '' : $kind . "\n")
            . "Gebucht: $people\nLehrkraft: Ada Example\n"
            . ($accompanist ? "Korrepetition: Ben Example\n" : '')
            . "Raum: $room";

        return self::lesson(
            $id,
            'bookings',
            $instant,
            ($canceled === '' ? '' : 'Abgesagt: ') . 'Imaginary Ballet – ' . $people,
            $room . ($elsewhere ? self::ADDRESS : ''),
            $description,
            $canceled !== '',
            $duration,
        );
    }

    private static function schedule(
        string $id,
        string $instant,
        bool $elsewhere = false,
        bool $canceled = false,
        bool $workshop = false,
        bool $locationFeed = false,
    ): ICalendarEvent {
        $room = $elsewhere ? self::OTHER_ROOM : self::ROOM;

        return self::lesson(
            $id,
            $locationFeed ? 'schedule-00000000-0000-7000-8000-000000000013' : 'schedule',
            $instant,
            ($canceled ? 'Canceled: ' : '') . 'Imaginary Ballet – Ada Example',
            $room . ($elsewhere ? self::ADDRESS : ''),
            ($canceled ? "This lesson is canceled.\n" : '') . ($workshop ? "Workshop\n" : '') . "Instructor: Ada Example\nRoom: $room",
            $canceled,
        );
    }

    private static function teaching(
        string $id,
        string $instant,
        string $instructor = 'Ada Example',
        string $accompanist = '',
        bool $canceled = false,
    ): ICalendarEvent {
        $role = $instructor === 'Ada Example' ? 'instructor' : 'accompanist';

        return self::lesson(
            $id,
            'teaching',
            $instant,
            ($canceled ? 'Canceled: ' : '') . "Imaginary Ballet – $instructor ($role)",
            self::ROOM,
            ($canceled ? "This lesson is canceled.\n" : '') . "Instructor: $instructor\n"
            . ($accompanist === '' ? '' : "Accompanist: $accompanist\n") . 'Room: ' . self::ROOM,
            $canceled,
        );
    }

    private static function lesson(
        string $id,
        string $scope,
        string $instant,
        string $summary,
        string $location,
        string $description,
        bool $canceled,
        int $duration = 60,
    ): ICalendarEvent {
        $lessonId = '00000000-0000-7000-8000-0000000000' . $id;
        $start = new DateTimeImmutable($instant);

        return new ICalendarEvent(
            uid: "$lessonId-$scope@school.example",
            start: $start,
            end: $start->modify("+$duration minutes"),
            summary: $summary,
            status: $canceled ? ICalendarStatus::Cancelled : ICalendarStatus::Confirmed,
            location: $location,
            description: $description,
            url: 'https://school.example/schedule?from=' . $start->format('Y-m-d') . "&lesson=$lessonId",
        );
    }
}
