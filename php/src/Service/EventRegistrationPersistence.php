<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use StudioSchedule\Domain\Event;
use StudioSchedule\Domain\EventRegistration;

/**
 * @phpstan-import-type Recipient from EventWaitlistPersistence
 * @phpstan-type Guest array{name:string,email:string}
 */
interface EventRegistrationPersistence
{
    /** @phpstan-impure */
    public function find(string $id): ?EventRegistration;

    public function forReceipt(string $receipt): ?EventRegistration;

    public function lockGuest(string $id): ?EventRegistration;

    public function inFamily(string $guardianId, string $personId, bool $forUpdate = false): bool;

    public function lockPerson(string $personId, bool $liveOnly): bool;

    public function requireSchool(): void;

    /** @param Guest|null $guest */
    public function create(
        Event $event,
        ?string $personId,
        ?array $guest,
        string $actorId,
        bool $waiting,
        DateTimeImmutable $now,
    ): EventRegistration;

    /** @return Recipient */
    public function actingRecipient(string $accountId, string $participant): array;

    public function markPaid(string $id, string $receiptId, DateTimeImmutable $now): EventRegistration;

    public function cancel(string $id, DateTimeImmutable $now): EventRegistration;

    public function markRefunded(string $id, string $receiptId, DateTimeImmutable $now): EventRegistration;

    public function audit(EventRegistration $registration, string $actorId, string $action, DateTimeImmutable $now, bool $refund = false): void;

    /** @return list<EventRegistration> */
    public function forEvent(string $eventId): array;

    /** @return list<EventRegistration> */
    public function forFamily(string $personId): array;

    /** @return array{eventId:string,givenName:string,expiresAt:DateTimeImmutable}|null */
    public function activeInvite(string $tokenHash, DateTimeImmutable $now): ?array;
}
