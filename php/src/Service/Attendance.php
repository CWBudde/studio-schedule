<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Date;

/**
 * @phpstan-import-type RosterLesson from ParticipantReadPersistence
 * @phpstan-import-type Booking from BookingPersistence
 */
final readonly class Attendance
{
    public function __construct(private BookingPersistence $store, private ClockInterface $clock) {}

    /** @return array<string,mixed> */
    public function set(SessionPrincipal $principal, string $id, string $status): array
    {
        if (!in_array($status, ['attended', 'no_show', 'booked'], true)) {
            throw CatalogFailure::field('status', 'invalid attendance status');
        }

        return $this->store->write(function () use ($principal, $id, $status): array {
            $lessonId = $this->store->bookingLesson($id) ?? throw self::notFound();
            $lesson = $this->lock($lessonId, $principal);
            $booking = $this->store->booking($id);
            if ($booking === null || $booking['lessonId'] !== $lessonId) {
                throw self::notFound();
            }
            if ($booking['status'] === 'canceled') {
                throw new CatalogFailure(409, 'the booking is canceled already', reason: 'already-canceled');
            }

            $now = $this->clock->now();
            $zone = $this->requireWindow($lesson, $principal, $now, 'path.bookingId');
            $changed = $this->change($lesson, [$booking], $status, $principal, $zone, $now);

            return $changed[0] ?? self::document($booking);
        });
    }

    /** @return list<array<string,mixed>> */
    public function markAll(SessionPrincipal $principal, string $id): array
    {
        return $this->store->write(function () use ($principal, $id): array {
            $lesson = $this->lock($id, $principal);
            $now = $this->clock->now();
            $zone = $this->requireWindow($lesson, $principal, $now, 'path.lessonId');

            return $this->change($lesson, $this->store->unknownAttendance($id), 'attended', $principal, $zone, $now);
        });
    }

    /** @return RosterLesson */
    private function lock(string $id, SessionPrincipal $principal): array
    {
        $staff = $principal->role === 'instructor' ? $principal->personId : null;

        return $this->store->lockLesson($id, $staff) ?? throw self::notFound();
    }

    /** @param RosterLesson $lesson */
    private function requireWindow(array $lesson, SessionPrincipal $principal, DateTimeImmutable $now, string $location): DateTimeZone
    {
        if ($lesson['status'] === 'canceled') {
            throw new CatalogFailure(409, 'the lesson cannot be booked', reason: 'closed', why: 'canceled');
        }
        if ($now < $lesson['startsAt']) {
            throw new CatalogFailure(422, 'the lesson has not started', $location, 'not-started');
        }
        $zone = new DateTimeZone($lesson['timezone']);
        if ($principal->role === 'instructor' && $now >= ScheduleService::dayOverAt($lesson['startsAt'], $lesson['endsAt'], $zone)) {
            throw new CatalogFailure(422, "the lesson's day is over; the office changes it now", $location, 'day-over');
        }

        return $zone;
    }

    /**
     * @param RosterLesson $lesson
     * @param list<Booking> $bookings
     * @return list<array<string,mixed>>
     */
    private function change(array $lesson, array $bookings, string $status, SessionPrincipal $principal, DateTimeZone $zone, DateTimeImmutable $now): array
    {
        $changed = [];
        $charges = [];
        $refunds = [];
        foreach ($bookings as $booking) {
            $previous = $booking['status'];
            if ($previous === $status) {
                continue;
            }

            self::requireValidBooking($booking);
            $this->store->setStatus($booking['id'], $status);
            $booking['status'] = $status;
            $changed[] = $booking;
            $person = $booking['personId'];
            if ($lesson['mode'] === 'attendance' && $person !== null && $booking['source'] !== 'member' && !$booking['trial']) {
                if ($previous === 'booked') {
                    $charges[] = ['id' => $booking['id'], 'personId' => $person];
                } elseif ($status === 'booked') {
                    $refunds[] = $booking['id'];
                }
            }
        }
        if ($charges !== [] || $refunds !== []) {
            $this->store->settle($charges, $refunds, (string) Date::fromInstant($lesson['startsAt'], $zone), $now);
        }

        $actor = $principal->impersonatorId ?? $principal->accountId;
        foreach ($changed as $booking) {
            $this->store->record($booking['id'], $actor, $status, $now);
        }

        return array_map(self::document(...), $changed);
    }

    /** @param Booking $booking */
    private static function requireValidBooking(array $booking): void
    {
        $name = $booking['guestName'];
        if ($name !== null && ($name === '' || SetupInput::trim($name) !== $name)) {
            throw CatalogFailure::field('guest.name', 'expected a nonblank guest name without surrounding space');
        }
        $email = $booking['guestEmail'];
        if ($email !== null && $email !== '' && !SetupInput::validEmail($email)) {
            throw CatalogFailure::field('guest.email', 'invalid guest email');
        }
        $note = $booking['trialNote'];
        if ($note !== null && (mb_strlen($note, 'UTF-8') > 500 || SetupInput::trim($note) !== $note)) {
            throw CatalogFailure::field('trialNote', 'expected at most 500 characters without surrounding space');
        }
    }

    /**
     * @param Booking $booking
     * @return array<string,mixed>
     */
    private static function document(array $booking): array
    {
        return array_filter($booking, static fn(mixed $value): bool => $value !== null && $value !== '');
    }

    private static function notFound(): CatalogFailure
    {
        return new CatalogFailure(404, 'resource not found');
    }
}
