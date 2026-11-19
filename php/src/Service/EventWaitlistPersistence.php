<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use StudioSchedule\Mail\Rendered;

/**
 * @phpstan-type Entry array{id:string,personId:?string}
 * @phpstan-type Recipient array{email:string,locale:string,givenName:string,participant:string,accountId:?string}
 * @phpstan-type Settings array{name:string,locale:string,timezone:DateTimeZone,currency:string,inviteMicros:int}
 * @phpstan-type TokenOffer array{id:string,eventId:string,personId:?string,expiresAt:DateTimeImmutable,payment:string,price:int}
 */
interface EventWaitlistPersistence
{
    /** @return list<string> */
    public function dueEvents(DateTimeImmutable $now): array;

    /** @return list<Entry> */
    public function dueInvites(string $eventId, DateTimeImmutable $now): array;

    /** @return list<Entry> */
    public function waiting(string $eventId): array;

    /** @return Settings */
    public function settings(): array;

    /**
     * @param list<string> $personIds
     * @return array<string,list<Recipient>>
     */
    public function recipients(array $personIds): array;

    /** @return TokenOffer|null */
    public function byTokenHash(string $tokenHash): ?array;

    public function lockLivePerson(string $personId): bool;

    public function finishInvite(string $id, DateTimeImmutable $now, bool $accept): void;

    public function invite(string $id, string $tokenHash, DateTimeImmutable $now, DateTimeImmutable $expires): void;

    public function enqueueMail(string $email, string $template, Rendered $mail, DateTimeImmutable $now): void;

    /** @return list<string> Subscriptions locked until the enclosing transaction commits. */
    public function pushSubscriptions(string $accountId): array;

    /** @param list<string> $subscriptionIds */
    public function enqueuePush(array $subscriptionIds, string $payload, DateTimeImmutable $now, DateTimeImmutable $expires): void;
}
