<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Domain\EventRegistration;
use StudioSchedule\Domain\Id;
use StudioSchedule\Domain\Instant;

final readonly class EventRegistrationReads
{
    public function __construct(
        private EventPersistence $events,
        private EventRegistrationPersistence $registrations,
        private ClockInterface $clock,
    ) {}

    /** @return list<array<string,mixed>>|null */
    public function office(string $eventId): ?array
    {
        return $this->events->inReadSnapshot(function () use ($eventId): ?array {
            if ($this->events->officeEvent($eventId) === null) {
                return null;
            }

            return array_map(self::officeDocument(...), $this->registrations->forEvent($eventId));
        });
    }

    /** @return list<array<string,mixed>> */
    public function mine(string $personId): array
    {
        if (!Id::valid($personId)) {
            return [];
        }

        return $this->events->inReadSnapshot(function () use ($personId): array {
            $registrations = $this->registrations->forFamily($personId);
            $ids = array_values(array_unique(array_map(static fn(EventRegistration $r): string => $r->eventId, $registrations)));
            $now = $this->clock->now();

            $documents = [];
            $first = [];
            foreach (array_chunk($ids, 1000) as $batch) {
                $sessions = [];
                foreach ($this->events->sessions($batch) as $session) {
                    assert($session->eventId !== null);
                    $sessions[$session->eventId][] = $session;
                }
                $occupied = $this->events->occupiedPlaces($batch, $now);
                foreach ($this->events->byIds($batch) as $event) {
                    $eventSessions = $sessions[$event->id] ?? [];
                    $first[$event->id] = $eventSessions[0]->startsAt ?? null;
                    $documents[$event->id] = EventService::publicDocument($event, $eventSessions, $occupied[$event->id] ?? 0, $now);
                }
            }

            usort($registrations, static fn(EventRegistration $a, EventRegistration $b): int
                => self::sortKey($a, $first[$a->eventId] ?? null) <=> self::sortKey($b, $first[$b->eventId] ?? null));

            $items = [];
            foreach ($registrations as $registration) {
                $event = $documents[$registration->eventId] ?? null;
                if ($event !== null) {
                    $items[] = [
                        'event' => $event,
                        'registration' => self::myDocument($registration, $event, $now),
                    ];
                }
            }

            return $items;
        });
    }

    /** @return array<string,mixed>|null */
    public function invite(#[SensitiveParameter] string $token): ?array
    {
        $tokenHash = hash('sha256', $token);

        return $this->events->inReadSnapshot(function () use ($tokenHash): ?array {
            $now = $this->clock->now();
            $invite = $this->registrations->activeInvite($tokenHash, $now);
            if ($invite === null) {
                return null;
            }
            $event = $this->events->officeEvent($invite['eventId']);
            if ($event === null) {
                return null;
            }

            $sessions = $this->events->sessions([$event->id]);
            $occupied = $this->events->occupiedPlaces([$event->id], $now);
            $public = EventService::publicDocument($event, $sessions, $occupied[$event->id] ?? 0, $now);
            if ($public['signupOpen'] !== true) {
                return null;
            }

            return ['givenName' => $invite['givenName'], 'expiresAt' => Instant::format($invite['expiresAt']), 'event' => $public];
        });
    }

    /** @return array{bool,bool,?DateTimeImmutable,DateTimeImmutable,string} */
    private static function sortKey(EventRegistration $registration, ?DateTimeImmutable $first): array
    {
        return [$registration->status === 'canceled', $first === null, $first, $registration->createdAt, $registration->id];
    }

    /** @return array<string,mixed> */
    private static function officeDocument(EventRegistration $registration): array
    {
        return self::baseDocument($registration) + [
            'eventId' => $registration->eventId,
            'vatRate' => $registration->vatRate,
            'updatedAt' => Instant::format($registration->updatedAt),
        ] + array_filter([
            'person' => $registration->person,
            'guest' => $registration->guest,
            'inviteExpiresAt' => self::optionalInstant($registration->expiresAt),
        ], static fn(mixed $value): bool => $value !== null);
    }

    /**
     * @param array<string,mixed> $event
     * @return array<string,mixed>
     */
    private static function myDocument(EventRegistration $registration, array $event, DateTimeImmutable $now): array
    {
        assert($registration->person !== null);
        $deadline = $event['cancelUntil'] ?? null;
        $cancelable = $registration->status === 'registered' && $event['status'] === 'published'
            && is_string($deadline) && $now < new DateTimeImmutable($deadline);

        return self::baseDocument($registration) + [
            'person' => $registration->person,
            'cancelable' => $cancelable,
        ] + ($registration->expiresAt === null ? [] : ['expiresAt' => Instant::format($registration->expiresAt)]);
    }

    /** @return array<string,mixed> */
    private static function baseDocument(EventRegistration $registration): array
    {
        return [
            'id' => $registration->id,
            'status' => $registration->status,
            'payment' => $registration->payment,
            'price' => $registration->price,
            'createdAt' => Instant::format($registration->createdAt),
        ] + array_filter([
            'receiptId' => $registration->receiptId,
            'cancellationReceiptId' => $registration->cancellationReceiptId,
            'canceledAt' => self::optionalInstant($registration->canceledAt),
        ], static fn(mixed $value): bool => $value !== null);
    }

    private static function optionalInstant(?DateTimeImmutable $value): ?string
    {
        return $value === null ? null : Instant::format($value);
    }
}
