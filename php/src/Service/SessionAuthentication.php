<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateInterval;
use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use StudioSchedule\Store\SessionStore;

final readonly class SessionAuthentication
{
    public function __construct(private SessionStore $store, private ClockInterface $clock) {}

    /** @return array{principal:SessionPrincipal,renewedUntil:?DateTimeImmutable}|null */
    public function authenticate(#[\SensitiveParameter] string $token): ?array
    {
        $digest = hash('sha256', $token);
        $session = $this->store->findActiveAccount($digest);
        $now = $this->clock->now()->setTimezone(new DateTimeZone('UTC'));
        if ($session === null || $now >= self::sessionEnd($session)) {
            return null;
        }

        if ($session->principal->impersonatorId !== null
            || $now < $session->lastSeenAt->setTimezone(new DateTimeZone('UTC'))->add(new DateInterval('PT24H'))) {
            return ['principal' => $session->principal, 'renewedUntil' => null];
        }

        $expires = min($now->add(new DateInterval('PT336H')), self::maximumAge($session));
        if ($this->store->renew($digest, $session->lastSeenAt, $now, $expires)) {
            return ['principal' => $session->principal, 'renewedUntil' => $expires];
        }

        // A competing request may have renewed the cookie or ended the session.
        $current = $this->store->findActiveAccount($digest);
        if ($current === null || $now >= self::sessionEnd($current)) {
            return null;
        }

        return ['principal' => $current->principal, 'renewedUntil' => self::sessionEnd($current)];
    }

    private static function sessionEnd(SessionRecord $session): DateTimeImmutable
    {
        return min($session->expiresAt, self::maximumAge($session));
    }

    private static function maximumAge(SessionRecord $session): DateTimeImmutable
    {
        // Timestamps may be returned using the PostgreSQL session's time zone.
        return $session->createdAt->setTimezone(new DateTimeZone('UTC'))->add(new DateInterval('PT720H'));
    }
}
