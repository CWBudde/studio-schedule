<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;

/** @phpstan-import-type Party from ScheduleRecipients */
interface LessonWritePersistence
{
    public function lock(string $id): bool;

    public function hasBookings(string $id): bool;

    public function delete(string $id): void;

    public function redemptionOnBooking(): bool;

    public function cancelLocked(string $id, string $actor, string $note, DateTimeImmutable $now, bool $onBooking): void;

    public function updateLocked(string $id, OneOffLessonData $data, DateTimeImmutable $start, DateTimeImmutable $end): void;

    public function recheckPayments(string $id, string $day, DateTimeImmutable $now): void;

    public function restoreLocked(string $id, string $day, DateTimeImmutable $now, bool $onBooking): void;

    public function inviteLocked(string $entryId, string $tokenHash, DateTimeImmutable $now, DateTimeImmutable $expires): void;

    /** @return list<Party> */
    public function bookedRecipients(string $id): array;
}
