<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\CalendarFeed;
use StudioSchedule\Domain\CalendarFeedScope;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\ICalendarDocument;
use StudioSchedule\Domain\ICalendarEvent;
use StudioSchedule\Domain\ICalendarStatus;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Store\CalendarFeedStore;
use StudioSchedule\Store\ScheduleStore;

final readonly class CalendarFeedRendering
{
    private const array EN = [
        'bookings' => 'My lessons', 'teaching' => 'Teaching', 'schedule' => 'Schedule',
        'booked' => 'Booked', 'instructor' => 'Instructor', 'accompanist' => 'Accompanist', 'room' => 'Room',
        'canceled' => 'Canceled', 'canceledNote' => 'This lesson is canceled.',
        'workshop' => 'Workshop', 'private' => 'Private lesson', 'rental' => 'Room rental', 'renter' => 'Rented by',
        'teaches' => 'instructor', 'accompanies' => 'accompanist',
    ];
    private const array DE = [
        'bookings' => 'Meine Stunden', 'teaching' => 'Unterricht', 'schedule' => 'Stundenplan',
        'booked' => 'Gebucht', 'instructor' => 'Lehrkraft', 'accompanist' => 'Korrepetition', 'room' => 'Raum',
        'canceled' => 'Abgesagt', 'canceledNote' => 'Diese Stunde fällt aus.',
        'workshop' => 'Workshop', 'private' => 'Privatstunde', 'rental' => 'Raumvermietung', 'renter' => 'Gemietet von',
        'teaches' => 'Lehrkraft', 'accompanies' => 'Korrepetition',
    ];

    public function __construct(
        private CalendarFeedStore $feeds,
        private ScheduleStore $schedule,
        private ClockInterface $clock,
        private string $baseURL,
    ) {}

    /** @return array{document:ICalendarDocument,filename:string}|null */
    public function render(#[\SensitiveParameter] string $token): ?array
    {
        if ($token === '' || strlen($token) > 128) {
            return null;
        }
        $now = $this->clock->now();
        $digest = hash('sha256', $token);
        $result = $this->schedule->inReadSnapshot(function () use ($digest, $now): ?array {
            $subscription = $this->feeds->activeByTokenHash($digest);
            if ($subscription === null) {
                return null;
            }
            ['feed' => $feed, 'owner' => $owner, 'locale' => $locale] = $subscription;

            $school = $this->feeds->school();
            $rules = $this->schedule->rules();
            $zone = $rules->timezone;
            $today = Date::fromInstant($now, $zone);
            $todayAt = $rules->midnight($today);
            $from = $rules->midnight($today->addDays(-28));
            $until = $rules->midnight($rules->generationEnd($today, $school['longestVisibleDays']));

            $labels = self::labels($locale ?: $school['locale']);
            $events = match ($feed->scope) {
                CalendarFeedScope::Bookings => $this->bookedEvents($feed, $owner, $from, $zone, $labels),
                CalendarFeedScope::Teaching => $this->teachingEvents($feed, $owner, $from, $until, $zone, $labels),
                CalendarFeedScope::Schedule => $this->scheduleEvents(
                    $feed,
                    $owner,
                    $from,
                    $until,
                    ScheduleService::visibleUntil($owner, $rules, $rules->releaseWindow($now)['releasedUntil'], $now),
                    $zone,
                    $labels,
                ),
            };
            $location = $feed->locationName ?? '';
            $scope = $feed->scope->value;
            $subject = $labels[$scope] . ($location === '' ? '' : ' (' . $location . ')');
            $filename = self::filename($school['name'] . '-' . $scope . ($location === '' ? '' : '-' . $location));

            return [
                'feedId' => $feed->id,
                'touch' => $feed->lastUsedAt === null || $feed->lastUsedAt < $todayAt,
                'todayAt' => $todayAt,
                'document' => (new ICalendarWriter())->encode($school['name'] . ' – ' . $subject, $zone, $now, $events),
                'filename' => $filename,
            ];
        });
        if ($result === null) {
            return null;
        }

        if ($result['touch']) {
            $this->feeds->touch($result['feedId'], $now, $result['todayAt']);
        }

        return ['document' => $result['document'], 'filename' => $result['filename']];
    }

    /**
     * @param array<string,string> $labels
     * @return list<ICalendarEvent>
     */
    private function bookedEvents(
        CalendarFeed $feed,
        SessionPrincipal $owner,
        DateTimeImmutable $from,
        DateTimeZone $zone,
        array $labels,
    ): array {
        $family = $this->schedule->family($owner->personId);
        if ($family === []) {
            return [];
        }
        $booked = $this->schedule->bookedPeople($from, array_keys($family));
        $events = [];
        foreach ($this->schedule->lessonsById(array_keys($booked)) as $lesson) {
            $names = array_intersect_key($family, $booked[$lesson->id]);

            $events[] = $this->event($lesson, $feed, $zone, $labels, implode(', ', $names));
        }

        return $events;
    }

    /**
     * @param array<string,string> $labels
     * @return list<ICalendarEvent>
     */
    private function teachingEvents(
        CalendarFeed $feed,
        SessionPrincipal $owner,
        DateTimeImmutable $from,
        DateTimeImmutable $until,
        DateTimeZone $zone,
        array $labels,
    ): array {
        if (!in_array($owner->role, ['instructor', 'office', 'admin'], true)) {
            return [];
        }
        $staff = $this->schedule->activeStaff($owner->personId);
        if ($staff === []) {
            return [];
        }
        $events = [];
        foreach ($this->schedule->teachingLessons($from, $until, array_keys($staff)) as $lesson) {
            $part = isset($staff[$lesson->instructor['id'] ?? '']) ? $labels['teaches'] : $labels['accompanies'];
            $events[] = $this->event($lesson, $feed, $zone, $labels, role: $part);
        }

        return $events;
    }

    /**
     * @param array<string,string> $labels
     * @return list<ICalendarEvent>
     */
    private function scheduleEvents(
        CalendarFeed $feed,
        SessionPrincipal $owner,
        DateTimeImmutable $from,
        DateTimeImmutable $until,
        ?DateTimeImmutable $visibleUntil,
        DateTimeZone $zone,
        array $labels,
    ): array {
        $filters = $feed->locationId === null ? [] : ['location' => $feed->locationId];
        $events = [];
        foreach ($this->schedule->lessons($from, $until, $filters) as $lesson) {
            if (ScheduleService::isVisible($lesson, $owner, $visibleUntil)) {
                $events[] = $this->event($lesson, $feed, $zone, $labels);
            }
        }

        return $events;
    }

    /** @param array<string,string> $labels */
    private function event(
        PublishedLesson $lesson,
        CalendarFeed $feed,
        DateTimeZone $zone,
        array $labels,
        string $booked = '',
        string $role = '',
    ): ICalendarEvent {
        $rental = $lesson->kind === 'rental';
        $canceled = $lesson->status === 'canceled';
        $instructor = $lesson->instructor['displayName'] ?? '';
        $who = match (true) {
            $feed->scope === CalendarFeedScope::Bookings => $booked,
            $rental => $lesson->renter ?? '',
            default => $instructor,
        };

        $summary = ($canceled ? $labels['canceled'] . ': ' : '') . ($lesson->courseType['name'] ?? $labels['rental'])
            . ' – ' . $who . ($role === '' ? '' : ' (' . $role . ')');
        $place = $lesson->location['name'] . ', ' . $lesson->room['name'];
        $address = array_filter(array_map('trim', explode("\n", $lesson->locationAddress ?? '')), static fn(string $s): bool => $s !== '');

        $desc = [];
        if ($canceled) {
            $note = $lesson->cancelNote;
            $desc[] = $note === null || $note === '' ? $labels['canceledNote'] : $note;
        }
        if ($lesson->kind !== 'regular') {
            $desc[] = $labels[$lesson->kind] ?? $labels['workshop'];
        }
        if ($feed->scope === CalendarFeedScope::Bookings) {
            $desc[] = $labels['booked'] . ': ' . $booked;
        }
        $desc[] = $rental ? $labels['renter'] . ': ' . $lesson->renter : $labels['instructor'] . ': ' . $instructor;
        if ($lesson->accompanist !== null) {
            $desc[] = $labels['accompanist'] . ': ' . $lesson->accompanist['displayName'];
        }
        $desc[] = $labels['room'] . ': ' . $place;

        $host = parse_url($this->baseURL, PHP_URL_HOST);
        $uid = $lesson->id . '-' . $feed->scope->value . ($feed->locationId === null ? '' : '-' . $feed->locationId)
            . '@' . (is_string($host) ? trim($host, '[]') : 'studio-schedule');
        $url = $this->baseURL === '' ? '' : $this->baseURL . '/schedule?' . http_build_query([
            'from' => (string) Date::fromInstant($lesson->startsAt, $zone),
            'lesson' => $lesson->id,
        ]);

        return new ICalendarEvent(
            $uid,
            $lesson->startsAt,
            $lesson->endsAt,
            $summary,
            $canceled ? ICalendarStatus::Cancelled : ICalendarStatus::Confirmed,
            $place . ($address === [] ? '' : ', ' . implode(', ', $address)),
            implode("\n", $desc),
            $url,
        );
    }

    /** @return array<string,string> */
    private static function labels(string $locale): array
    {
        $language = strtok(strtolower($locale), '-_');

        return $language === 'de' ? self::DE : self::EN;
    }

    private static function filename(string $name): string
    {
        $base = trim(preg_replace('/[^a-z0-9]+/', '-', mb_strtolower($name, 'UTF-8')) ?? '', '-');

        return ($base === '' ? 'calendar' : $base) . '.ics';
    }
}
