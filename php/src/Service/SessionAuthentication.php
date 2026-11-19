<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateInterval;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use StudioSchedule\Store\SessionStore;

final readonly class SessionAuthentication
{
    public function __construct(private SessionStore $store, private ClockInterface $clock) {}

    /** @return array{principal:SessionPrincipal,renewedUntil:?\DateTimeImmutable}|null */
    public function authenticate(#[\SensitiveParameter] string $token): ?array
    {
        $digest = hash('sha256', $token);
        $session = $this->store->findActiveAccount($digest);
        $now = $this->clock->now()->setTimezone(new DateTimeZone('UTC'));
        if ($session === null || $now >= $session->endsAt()) {
            return null;
        }

        if ($session->principal->impersonatorId !== null
            || $now < $session->lastSeenAt->setTimezone(new DateTimeZone('UTC'))->add(new DateInterval('PT24H'))) {
            return ['principal' => $session->principal, 'renewedUntil' => null];
        }

        $expires = min($now->add(new DateInterval(SessionRecord::IDLE_TIMEOUT)), $session->maximumEnd());
        if ($this->store->renew($digest, $session->lastSeenAt, $now, $expires)) {
            return ['principal' => $session->principal, 'renewedUntil' => $expires];
        }

        // A competing request may have renewed the cookie or ended the session.
        $current = $this->store->findActiveAccount($digest);
        if ($current === null || $now >= $current->endsAt()) {
            return null;
        }

        return ['principal' => $current->principal, 'renewedUntil' => $current->endsAt()];
    }
}
