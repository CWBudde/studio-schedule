<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\EventRegistration;

/** @phpstan-import-type Settings from ReceiptWritePersistence */
final readonly class EventCancellations
{
    /**
     * @param Closure():EventWaitlist $waitlist
     * @param (Closure():void)|null $wakeMail
     */
    public function __construct(
        private EventPersistence $events,
        private EventRegistrationPersistence $registrations,
        private ReceiptIssuance $receipts,
        private ClockInterface $clock,
        private Closure $waitlist,
        private ?Closure $wakeMail = null,
    ) {}

    /** @return array<string,mixed> */
    public function member(SessionPrincipal $principal, string $id): array
    {
        return $this->cancel($principal, $id, member: true, refund: true);
    }

    /** @return array<string,mixed> */
    public function office(SessionPrincipal $principal, string $id, bool $refund): array
    {
        return $this->cancel($principal, $id, member: false, refund: $refund);
    }

    /** @return array<string,mixed> */
    public function wholeEvent(SessionPrincipal $principal, string $id, string $note): array
    {
        $result = $this->events->write(function () use ($principal, $id, $note): array {
            $settings = $this->receipts->settings();
            $event = $this->events->lockEvent($id) ?? throw self::notFound();
            if ($event->status === 'canceled') {
                throw new CatalogFailure(409, 'the event takes no registrations', reason: 'closed');
            }

            $now = $this->clock->now();
            $actor = $principal->impersonatorId ?? $principal->accountId;
            $this->events->cancelSessions($id, $this->events->sessions([$id]), $actor, $note, $now);

            $canceled = [];
            foreach ($this->registrations->forEvent($id) as $registration) {
                if ($registration->status === 'canceled' && ($registration->payment !== 'paid' || $registration->receiptId === null)) {
                    continue;
                }
                $updated = $this->cancelLocked($registration, $actor, $now, $settings, refund: true);
                $canceled[] = ['registration' => $updated, 'refunded' => $registration->payment === 'paid' && $updated->payment === 'refunded'];
            }
            $event = $this->events->setStatus($id, 'canceled', $now);
            $sessions = $this->events->sessions([$id]);
            if ($canceled !== []) {
                ($this->waitlist)()->sendEventCancellation($event, $sessions, $canceled, $note, $now, [
                    'name' => $settings['school_name'], 'locale' => $settings['default_locale'],
                    'timezone' => new DateTimeZone($settings['timezone']),
                ]);
            }
            $this->events->auditCancellation($event->id, $actor, $now);

            $counts = $this->events->registrationCounts([$id], $this->clock->now());
            $image = $this->events->imageInfo($id);

            return EventService::officeDocument($event, $sessions, $counts[$id] ?? null) + ($image === null ? [] : ['image' => $image]);
        });
        if ($this->wakeMail !== null) {
            ($this->wakeMail)();
        }

        return $result;
    }

    /** @return array<string,mixed> */
    private function cancel(SessionPrincipal $principal, string $id, bool $member, bool $refund): array
    {
        $result = $this->events->write(function () use ($principal, $id, $member, $refund): array {
            $settings = $this->receipts->settings();
            $observed = $this->registrations->find($id) ?? throw self::notFound();
            $event = $this->events->lockEvent($observed->eventId) ?? throw self::notFound();
            $registration = $this->registrations->find($id) ?? throw self::notFound();
            if ($member) {
                $person = $registration->person['id'] ?? throw self::notFound();
                if (!$this->registrations->inFamily($principal->personId, $person)
                    || !$this->registrations->lockPerson($person, liveOnly: false)
                    || !$this->registrations->inFamily($principal->personId, $person, forUpdate: true)) {
                    throw self::notFound();
                }
            }
            if ($registration->status === 'canceled') {
                throw new CatalogFailure(409, 'the registration is canceled already', reason: 'already-canceled');
            }

            $now = $this->clock->now();
            if ($member) {
                if ($registration->status !== 'registered') {
                    throw new CatalogFailure(409, 'the registration is on the waiting list', reason: 'not-registered');
                }
                [, $deadline] = EventService::signupWindow($event, $this->events->sessions([$event->id]), $now);
                if ($deadline === null || $now >= $deadline) {
                    throw new CatalogFailure(409, 'the cancellation deadline has passed', reason: 'too-late');
                }
            }

            $actor = $principal->impersonatorId ?? $principal->accountId;
            $updated = $this->cancelLocked($registration, $actor, $now, $settings, $refund);
            $refunded = $updated->payment === 'refunded' && $registration->payment === 'paid';
            $this->registrations->audit($updated, $actor, 'event-registration-canceled', $now, $refunded);
            $waitlist = ($this->waitlist)();
            if ($registration->status === 'registered' || $registration->status === 'invited') {
                $waitlist->offerLocked($event, $now);
            }
            if (!$member) {
                return EventRegistrationReads::officeDocument($updated);
            }

            assert($updated->person !== null);
            $recipient = $this->registrations->actingRecipient($principal->accountId, $updated->person['name']);
            $waitlist->sendCancellation($event, $now, $recipient, $refunded);
            $document = EventService::publicDocument(
                $event,
                $this->events->sessions([$event->id]),
                $this->events->occupiedPlaces([$event->id], $now)[$event->id] ?? 0,
                $now,
            );

            return ['registration' => EventRegistrationReads::myDocument($updated, $document, $now), 'event' => $document];
        });
        if ($this->wakeMail !== null) {
            ($this->wakeMail)();
        }

        return $result;
    }

    /**
     * End a registration or refund its fee under the caller's event lock.
     *
     * @param Settings $settings
     */
    public function cancelLocked(EventRegistration $registration, string $actor, DateTimeImmutable $now, array $settings, bool $refund): EventRegistration
    {
        if ($registration->status !== 'canceled') {
            $registration = $this->registrations->cancel($registration->id, $now);
        }
        if (!$refund || $registration->payment !== 'paid' || $registration->receiptId === null) {
            return $registration;
        }

        $at = $this->clock->now();
        $receipt = $this->receipts->refund($settings, $actor, $registration->receiptId, $at);

        return $this->registrations->markRefunded($registration->id, $receipt['id'], $at);
    }

    private static function notFound(): CatalogFailure
    {
        return new CatalogFailure(404, 'resource not found');
    }
}
