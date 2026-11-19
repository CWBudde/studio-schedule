<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use Psr\Clock\ClockInterface;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\EventRegistration;
use StudioSchedule\Store\PassLedgerStore;
use StudioSchedule\Store\ReceiptReadStore;

final readonly class ReceiptCancellation
{
    public function __construct(
        private EventPersistence $events,
        private EventRegistrationPersistence $registrations,
        private PassLedgerStore $passes,
        private ReceiptWritePersistence $writeStore,
        private ReceiptReadStore $reads,
        private ReceiptIssuance $issuer,
        private ClockInterface $clock,
        private ?Closure $wakeMail = null,
    ) {}

    /** @return array<string,mixed> */
    public function cancel(SessionPrincipal $principal, string $id, #[SensitiveParameter] string $reason): array
    {
        $result = $this->events->write(function () use ($principal, $id, $reason): array {
            $settings = $this->issuer->settings();
            $original = $this->writeStore->original($id) ?? throw new CatalogFailure(404, 'resource not found');
            if ($original['kind'] === 'cancellation') {
                throw new CatalogFailure(409, 'a cancellation cannot be canceled', reason: 'cancellation');
            }
            if ($original['hasCancellation']) {
                throw new CatalogFailure(409, 'the receipt is canceled already', reason: 'already-canceled');
            }

            $this->passes->voidUnusedReceiptPass($id, $this->clock);
            $registration = $this->refundableRegistration($id);
            $actor = $principal->impersonatorId ?? $principal->accountId;
            $credit = $this->issuer->refund($settings, $actor, $id, $this->clock->now(), $reason);
            if ($registration !== null) {
                $this->registrations->markRefunded($registration->id, $credit['id'], $this->clock->now());
            }

            return $this->reads->detailInTransaction($credit['id']) ?? throw new RuntimeException('The new cancellation receipt is missing');
        });
        if ($this->wakeMail !== null) {
            ($this->wakeMail)();
        }

        return $result;
    }

    private function refundableRegistration(string $receipt): ?EventRegistration
    {
        $candidate = $this->registrations->forReceipt($receipt);
        if ($candidate === null) {
            return null;
        }
        $this->events->lockEvent($candidate->eventId) ?? throw new CatalogFailure(404, 'resource not found');
        $registration = $this->registrations->find($candidate->id) ?? throw new CatalogFailure(404, 'resource not found');
        if ($registration->status !== 'canceled') {
            throw new CatalogFailure(409, 'the registration is open; cancel it with a refund instead', reason: 'registration-open');
        }
        $sessions = $this->events->sessions([$registration->eventId]);
        $now = $this->clock->now();
        foreach ($sessions as $lesson) {
            if ($lesson->status === 'scheduled' && $lesson->startsAt <= $now) {
                throw new CatalogFailure(409, 'the event has started; cancel the registration with a refund instead', reason: 'event-attended');
            }
        }

        return $registration;
    }
}
