<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeZone;
use Psr\Clock\ClockInterface;
use RuntimeException;
use StudioSchedule\Domain\PublishedLesson;

/** @phpstan-import-type PassDocument from PassReadPersistence */
final readonly class PassRead
{
    public function __construct(private PassReadPersistence $store, private ClockInterface $clock) {}

    /** @return array{timezone:string,today:string,redemptionMode:string,items:list<array<string,mixed>>}|null */
    public function mine(string $person): ?array
    {
        return $this->store->read(function () use ($person): ?array {
            $school = $this->store->school();
            if ($school === null || !$this->store->personExists($person)) {
                return null;
            }
            $today = $this->today($school['timezone']);

            return [
                'timezone' => $school['timezone'], 'today' => $today, 'redemptionMode' => $school['redemption_mode'],
                'items' => $this->items($person, true, $today),
            ];
        });
    }

    /** @return list<array<string,mixed>>|null */
    public function person(string $person): ?array
    {
        return $this->store->read(function () use ($person): ?array {
            $school = $this->store->school();
            if ($school === null || !$this->store->personExists($person)) {
                return null;
            }

            return $this->items($person, false, $this->today($school['timezone']));
        });
    }

    private function today(string $timezone): string
    {
        return $this->clock->now()->setTimezone(new DateTimeZone($timezone))->format('Y-m-d');
    }

    /** @return list<array<string,mixed>> */
    private function items(string $person, bool $family, string $today): array
    {
        $passes = $this->store->passes($person, $family);
        if ($passes === []) {
            return [];
        }
        $history = $this->history($person, $family);
        $items = [];
        foreach ($passes as $pass) {
            $uses = $history[$pass['id']]['uses'] ?? [];
            $items[] = $pass + [
                'usable' => self::covers($pass, $today),
                'uses' => array_map(static function (array $use) use ($pass): array {
                    $lesson = ScheduleService::lessonSummary($use['lesson']);
                    // Go's pass history resolves lesson references without the public event slug.
                    unset($lesson['eventSlug']);

                    return [
                        'bookingId' => $use['bookingId'], 'personName' => $pass['owner']['name'],
                        'bookingStatus' => $use['bookingStatus'], 'late' => $use['late'], 'redeemedAt' => $use['redeemedAt'],
                        'lesson' => $lesson,
                    ];
                }, $uses),
                'useCount' => $history[$pass['id']]['count'] ?? 0,
            ];
        }
        // The store already orders each group by most recent sale and then by ID.
        usort($items, static fn(array $first, array $second): int => $second['usable'] <=> $first['usable']);

        return $items;
    }

    /** @param PassDocument $pass */
    private static function covers(array $pass, string $today): bool
    {
        return $pass['status'] === 'active' && $today >= $pass['validFrom']
            && (!isset($pass['validUntil']) || $today <= $pass['validUntil'])
            && ($pass['kind'] === 'time' || $pass['visitsRemaining'] > 0);
    }

    /**
     * @return array<string,array{
     *     count:int,uses:list<array{
     *         bookingId:string,bookingStatus:string,late:bool,redeemedAt:string,lesson:PublishedLesson
     *     }>
     * }>
     */
    private function history(string $person, bool $family): array
    {
        $redemptions = $this->store->redemptions($person, $family);
        $ids = array_values(array_unique(array_column($redemptions, 'lessonId')));
        $lessons = array_column($this->store->lessons($ids), null, 'id');
        $history = [];
        foreach ($redemptions as $row) {
            $lesson = $lessons[$row['lessonId']] ?? throw new RuntimeException('Missing lesson for pass redemption');
            $history[$row['passId']]['count'] = $row['useCount'];
            $history[$row['passId']]['uses'][] = [
                'bookingId' => $row['bookingId'], 'bookingStatus' => $row['bookingStatus'], 'late' => $row['late'],
                'redeemedAt' => $row['redeemedAt'], 'lesson' => $lesson,
            ];
        }

        foreach ($history as &$pass) {
            usort($pass['uses'], static fn(array $first, array $second): int
                => [$second['lesson']->startsAt, $second['lesson']->id] <=> [$first['lesson']->startsAt, $first['lesson']->id]);
        }
        unset($pass);

        return $history;
    }
}
