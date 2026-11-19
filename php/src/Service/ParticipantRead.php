<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Instant;

/**
 * @phpstan-import-type RosterLesson from ParticipantReadPersistence
 */
final readonly class ParticipantRead
{
    public function __construct(private ParticipantReadPersistence $store, private ClockInterface $clock) {}

    /** @return array<string, mixed>|null */
    public function roster(string $id, SessionPrincipal $principal): ?array
    {
        return $this->store->read(function () use ($id, $principal): ?array {
            $staff = $principal->role === 'instructor' ? $principal->personId : null;
            $lesson = $this->store->lesson($id, $staff);
            if ($lesson === null) {
                return null;
            }

            $now = $this->clock->now();
            $event = $lesson['eventId'];

            return [
                'lessonId' => $id,
                'capacity' => $lesson['capacity'],
                'dayOverAt' => self::dayOverAt($lesson),
            ] + ($event === null
                ? $this->lessonRoster($id, $lesson, $now)
                : $this->eventRoster($event, $lesson['capacity'], $now));
        });
    }

    /** @return list<array<string, mixed>>|null */
    public function history(string $booking): ?array
    {
        return $this->store->read(fn(): ?array => $this->store->bookingEvents($booking));
    }

    /** @param RosterLesson $lesson */
    private static function dayOverAt(array $lesson): string
    {
        return Instant::format(ScheduleService::dayOverAt($lesson['startsAt'], $lesson['endsAt'], new DateTimeZone($lesson['timezone'])));
    }

    /**
     * @param RosterLesson $lesson
     * @return array<string, mixed>
     */
    private function lessonRoster(string $id, array $lesson, DateTimeImmutable $now): array
    {
        $bookings = [];
        $waitlist = [];
        $active = $held = $waiting = 0;
        foreach ($this->store->bookings($id) as $row) {
            $payment = BookingPayment::document($row, $lesson['status'], $lesson['mode'] === 'booking');
            unset($row['pass']);
            $bookings[] = array_filter($row, static fn(mixed $field): bool => $field !== null && $field !== '') + ['payment' => $payment];
            if ($row['status'] !== 'canceled') {
                ++$active;
            }
        }
        foreach ($this->store->waitlist($id) as $row) {
            $expiry = $row['expiresAt'];
            unset($row['expiresAt']);
            if ($row['status'] === 'waiting') {
                $row['position'] = ++$waiting;
            } elseif ($row['status'] === 'invited') {
                if ($expiry !== null && $expiry > $now) {
                    $row['expiresAt'] = Instant::format($expiry);
                    ++$held;
                } else {
                    $row['status'] = 'expired';
                }
            }
            $waitlist[] = $row['anonymized'] === null ? array_diff_key($row, ['anonymized' => true]) : $row;
        }

        return ['active' => $active, 'held' => $held, 'waiting' => $waiting, 'freePlaces' => max(0, $lesson['capacity'] - $active - $held),
            'bookings' => $bookings, 'waitlist' => $waitlist];
    }

    /** @return array<string, mixed> */
    private function eventRoster(string $event, int $capacity, DateTimeImmutable $now): array
    {
        $held = $waiting = 0;
        $people = [];
        foreach ($this->store->registrations($event) as $row) {
            if ($row['status'] === 'registered') {
                unset($row['status'], $row['expiresAt']);
                if ($row['anonymized'] === null) {
                    unset($row['anonymized']);
                }
                $people[] = $row;
            } elseif ($row['status'] === 'waiting') {
                ++$waiting;
            } elseif ($row['status'] === 'invited' && $row['expiresAt'] !== null && $row['expiresAt'] > $now) {
                ++$held;
            }
        }
        $active = count($people);

        return ['active' => $active, 'held' => $held, 'waiting' => $waiting, 'freePlaces' => max(0, $capacity - $active - $held),
            'registrants' => $people, 'bookings' => [], 'waitlist' => []];
    }
}
