<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;

/**
 * @phpstan-import-type RosterLesson from ParticipantReadPersistence
 * @phpstan-type Booking array{id:string,lessonId:string,personId:?string,guestName:?string,guestEmail:?string,
 *     status:string,source:string,trial:bool,trialNote:?string,bookedAt:string,canceledAt:?string,late:bool}
 */
interface AttendancePersistence
{
    /**
     * @template T
     * @param Closure():T $write
     * @return T
     */
    public function write(Closure $write): mixed;

    public function bookingLesson(string $booking): ?string;

    /** @return RosterLesson|null */
    public function lockLesson(string $lesson, ?string $staffPerson): ?array;

    /** @return Booking|null */
    public function booking(string $id): ?array;

    /** @return list<Booking> */
    public function unknownAttendance(string $lesson): array;

    public function setStatus(string $booking, string $status): void;

    /**
     * @param list<array{id:string,personId:string}> $charges
     * @param list<string> $refunds Booking IDs.
     */
    public function settle(array $charges, array $refunds, string $schoolDate, DateTimeImmutable $now): void;

    public function record(string $booking, string $actor, string $status, DateTimeImmutable $now): void;
}
