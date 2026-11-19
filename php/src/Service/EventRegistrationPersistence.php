<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use StudioSchedule\Domain\EventRegistration;

interface EventRegistrationPersistence
{
    /** @return list<EventRegistration> */
    public function forEvent(string $eventId): array;

    /** @return list<EventRegistration> */
    public function forFamily(string $personId): array;

    /** @return array{eventId:string,givenName:string,expiresAt:DateTimeImmutable}|null */
    public function activeInvite(string $tokenHash, DateTimeImmutable $now): ?array;
}
