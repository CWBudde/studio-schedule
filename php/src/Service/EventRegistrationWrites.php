<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\Event;
use StudioSchedule\Domain\EventRegistration;
use StudioSchedule\Domain\PublishedLesson;

/** @phpstan-import-type Guest from EventRegistrationPersistence */
final readonly class EventRegistrationWrites
{
    public function __construct(
        private EventPersistence $events,
        private EventRegistrationPersistence $store,
        private EventWaitlist $waitlist,
        private ClockInterface $clock,
    ) {}

    /** @return array<string,mixed> */
    public function signup(SessionPrincipal $principal, string $slug, string $person, bool $waiting): array
    {
        $result = $this->events->write(function () use ($principal, $slug, $person, $waiting): array|CatalogFailure {
            $observed = $this->events->bySlug($slug) ?? throw self::notFound();
            if (!$waiting) {
                $this->store->requireSchool();
            }
            $event = $this->events->lockEvent($observed->id) ?? throw self::notFound();
            $this->requireFamily($principal, $person);
            $this->lockFamily($principal, $person, liveOnly: true);

            $now = $this->clock->now();
            $this->waitlist->settleLocked($event, $now);
            $sessions = $this->events->sessions([$event->id]);
            [, $cancelUntil, $open] = EventService::signupWindow($event, $sessions, $now);
            $occupied = $this->occupied($event, $now);
            // Commit any expired offers and promotions even when signup is refused.
            if (!$open) {
                return self::closed();
            }
            if ($waiting && $occupied < $event->capacity) {
                return new CatalogFailure(409, 'the event has a free place; register instead', reason: 'not-full');
            }
            if (!$waiting && $occupied >= $event->capacity) {
                return self::full();
            }

            $actor = $principal->impersonatorId ?? $principal->accountId;
            $registration = $this->store->create($event, $person, null, $actor, $waiting, $now);
            $this->store->audit($registration, $actor, 'event-registration-added', $now);
            if (!$waiting) {
                ++$occupied;
                $this->confirm($principal, $registration, $event, $sessions, $cancelUntil, $now);
            }

            return self::memberDocument($registration, $event, $sessions, $occupied, $now);
        });
        if ($result instanceof CatalogFailure) {
            throw $result;
        }

        return $result;
    }

    /**
     * @param Guest|null $guest
     * @return array<string,mixed>
     */
    public function desk(
        SessionPrincipal $principal,
        string $id,
        ?string $person,
        #[SensitiveParameter]
        ?array $guest,
        bool $confirmOverCapacity,
    ): array {
        $result = $this->events->write(function () use ($principal, $id, $person, $guest, $confirmOverCapacity): array|CatalogFailure {
            $event = $this->events->lockEvent($id) ?? throw self::notFound();
            if ($person !== null && !$this->store->lockPerson($person, liveOnly: true)) {
                throw CatalogFailure::field('personId', 'person does not exist');
            }
            if ($guest !== null) {
                if ($guest['name'] === '') {
                    throw CatalogFailure::field('guest.name', 'a guest name is required');
                }
                $anonymized = $guest['name'] === EventRegistration::ANONYMOUS_GUEST_NAME && $guest['email'] === '';
                if (!$anonymized && !SetupInput::validEmail($guest['email'])) {
                    throw CatalogFailure::field('guest.email', 'a valid email address is required');
                }
            }
            if ($event->status !== 'published') {
                throw self::closed();
            }

            $now = $this->clock->now();
            if (!$confirmOverCapacity) {
                $this->waitlist->settleLocked($event, $now);
                if ($this->occupied($event, $now) >= $event->capacity) {
                    return self::full();
                }
            }

            $actor = $principal->impersonatorId ?? $principal->accountId;
            $registration = $this->store->create($event, $person, $guest, $actor, false, $now);
            $this->store->audit($registration, $actor, 'event-registration-added', $now);

            return EventRegistrationReads::officeDocument($registration);
        });
        if ($result instanceof CatalogFailure) {
            throw $result;
        }

        return $result;
    }

    /**
     * @param 'accept'|'decline'|'leave' $action
     * @return array<string,mixed>
     */
    public function respond(SessionPrincipal $principal, string $id, string $action): array
    {
        $result = $this->events->write(function () use ($principal, $id, $action): array|CatalogFailure {
            $observed = $this->store->find($id) ?? throw self::notFound();
            $event = $this->events->lockEvent($observed->eventId) ?? throw self::notFound();
            // Wait for any earlier event write before checking the registration again.
            $registration = $this->store->find($id) ?? throw self::notFound();
            $person = $registration->person['id'] ?? throw self::notFound();
            $this->requireFamily($principal, $person);
            $now = $this->clock->now();

            $held = $registration->status === 'invited';
            if ($held && $registration->expiresAt !== null && $registration->expiresAt <= $now) {
                $this->lockFamily($principal, $person, liveOnly: false);
                $this->waitlist->settleLocked($event, $now);

                return self::expired();
            }
            self::requireActive($registration, $action);

            $accept = $action === 'accept';
            $sessions = $this->events->sessions([$event->id]);
            [, $cancelUntil, $open] = EventService::signupWindow($event, $sessions, $now);
            if ($accept && !$open) {
                throw self::closed();
            }
            $this->lockFamily($principal, $person, liveOnly: $accept);
            $this->waitlist->finishLocked($event, $id, $now, $accept, $held);
            $updated = $this->store->find($id) ?? throw new RuntimeException('The updated registration is missing');
            $this->store->audit($updated, $principal->impersonatorId ?? $principal->accountId, $accept ? 'event-registration-added' : 'event-registration-canceled', $now);
            if ($accept) {
                $this->confirm($principal, $updated, $event, $sessions, $cancelUntil, $now);
            }

            return self::memberDocument($updated, $event, $sessions, $this->occupied($event, $now), $now);
        });
        if ($result instanceof CatalogFailure) {
            throw $result;
        }

        return $result;
    }

    /** @param 'accept'|'decline'|'leave' $action */
    private static function requireActive(EventRegistration $registration, string $action): void
    {
        if ($action === 'leave') {
            if (!in_array($registration->status, ['waiting', 'invited'], true)) {
                throw new CatalogFailure(409, 'the entry is no longer on the waiting list', reason: 'not-waiting');
            }

            return;
        }

        match ($registration->status) {
            'waiting' => throw new CatalogFailure(409, 'the entry is waiting for an invite', reason: 'not-invited'),
            'registered' => throw new CatalogFailure(409, 'the invite has been used', reason: 'invite-used'),
            'canceled' => throw self::expired(),
            default => null,
        };
    }

    private function requireFamily(SessionPrincipal $principal, string $person): void
    {
        if (!$this->store->inFamily($principal->personId, $person)) {
            throw self::notFound();
        }
    }

    private function lockFamily(SessionPrincipal $principal, string $person, bool $liveOnly): void
    {
        // Guardian removal locks the person before it changes the family link.
        if (!$this->store->lockPerson($person, $liveOnly) || !$this->store->inFamily($principal->personId, $person, forUpdate: true)) {
            throw self::notFound();
        }
    }

    private function occupied(Event $event, DateTimeImmutable $now): int
    {
        return $this->events->occupiedPlaces([$event->id], $now)[$event->id] ?? 0;
    }

    /**
     * @param list<PublishedLesson> $sessions
     * @return array<string,mixed>
     */
    private static function memberDocument(EventRegistration $registration, Event $event, array $sessions, int $occupied, DateTimeImmutable $now): array
    {
        $document = EventService::publicDocument($event, $sessions, $occupied, $now);

        return ['registration' => EventRegistrationReads::myDocument($registration, $document, $now), 'event' => $document];
    }

    /** @param list<PublishedLesson> $sessions */
    private function confirm(
        SessionPrincipal $principal,
        EventRegistration $registration,
        Event $event,
        array $sessions,
        ?DateTimeImmutable $cancelUntil,
        DateTimeImmutable $now,
    ): void {
        assert($registration->person !== null);
        $recipient = $this->store->actingRecipient($principal->accountId, $registration->person['name']);
        $this->waitlist->sendConfirmation($event, $registration->payment, $registration->price, $sessions, $cancelUntil, $now, [$recipient]);
    }

    private static function notFound(): CatalogFailure
    {
        return new CatalogFailure(404, 'resource not found');
    }

    private static function closed(): CatalogFailure
    {
        return new CatalogFailure(409, 'the event takes no registrations', reason: 'closed');
    }

    private static function full(): CatalogFailure
    {
        return new CatalogFailure(409, 'the event is full', reason: 'full');
    }

    private static function expired(): CatalogFailure
    {
        return new CatalogFailure(409, 'the invite has expired', reason: 'invite-expired');
    }
}
