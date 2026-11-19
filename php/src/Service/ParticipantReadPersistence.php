<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;

/**
 * @phpstan-type RosterLesson array{capacity: int, status: string, eventId: ?string, mode: string,
 *     timezone: string, startsAt: DateTimeImmutable, endsAt: DateTimeImmutable}
 * @phpstan-type PaymentPass array{id: string, name: string, kind: string, visitsRemaining: int, validUntil?: string}
 * @phpstan-type RosterBooking array{bookingId: string, personId: ?string, name: string, guest: bool,
 *     anonymized: ?true, contactEmail: ?string, contactPhone: ?string, status: string, source: string, trial: bool,
 *     trialNote: ?string, late: bool, bookedAt: string, canceledAt: ?string, pass: ?PaymentPass}
 * @phpstan-type RosterWaiter array{entryId: string, personId: string, name: string, anonymized: ?true,
 *     status: string, stayOnList: bool, joinedAt: string, expiresAt: ?DateTimeImmutable}
 * @phpstan-type RosterRegistrant array{id: string, name: string, guest: bool, anonymized: ?true, status: string, expiresAt: ?DateTimeImmutable}
 */
interface ParticipantReadPersistence
{
    /**
     * @template T
     * @param Closure():T $read
     * @return T
     */
    public function read(Closure $read): mixed;

    /** @return RosterLesson|null */
    public function lesson(string $id, ?string $staffPerson): ?array;

    /** @return list<RosterBooking> */
    public function bookings(string $lesson): array;

    /** @return list<RosterWaiter> */
    public function waitlist(string $lesson): array;

    /** @return list<RosterRegistrant> */
    public function registrations(string $event): array;

    /** @return list<array<string, mixed>>|null */
    public function bookingEvents(string $booking): ?array;
}
