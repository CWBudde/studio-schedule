<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\EventRegistration;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Receipt\Document;

/**
 * @phpstan-import-type ReceiptRef from ReceiptIssuance
 * @phpstan-type Payment array{method:'cash'|'transfer'|'card',payerId:?string,name:string,address:string,email:string}
 */
final readonly class EventPayments
{
    public function __construct(
        private EventPersistence $events,
        private EventRegistrationPersistence $registrations,
        private ReceiptIssuance $receipts,
        private ClockInterface $clock,
        private ?Closure $wakeMail = null,
    ) {}

    /**
     * @param Payment $payment
     * @return array{registration:array<string,mixed>,receipt:ReceiptRef}
     */
    public function pay(SessionPrincipal $principal, string $id, #[SensitiveParameter] array $payment): array
    {
        $result = $this->events->write(function () use ($principal, $id, $payment): array {
            $settings = $this->receipts->settings();
            $observed = $this->registrations->find($id) ?? throw self::notFound();
            $event = $this->events->lockEvent($observed->eventId) ?? throw self::notFound();
            $registration = $this->registrations->find($id) ?? throw self::notFound();
            self::requireUnpaidPlace($registration);
            if ($registration->guest !== null) {
                // Retention can change guest details without locking the event.
                $registration = $this->registrations->lockGuest($id) ?? throw self::notFound();
            }

            $participant = $registration->person['name'] ?? $registration->guest['name'] ?? throw new RuntimeException('The registration has no participant');
            $payerId = $payment['payerId'] ?? $registration->person['id'] ?? null;
            $payer = null;
            if ($payerId !== null) {
                if ($registration->person === null) {
                    throw CatalogFailure::field('payerId', 'a guest pays for themself');
                }
                if (!$this->registrations->lockPerson($payerId, liveOnly: true)) {
                    throw CatalogFailure::field('payerId', 'person does not exist');
                }
                $payer = $this->receipts->payer($payerId);
            }
            ReceiptIssuance::requireIssuer($settings);

            $name = $payment['name'] !== '' ? $payment['name'] : ($payer === null ? $participant : $payer['given_name'] . ' ' . $payer['family_name']);
            if (SetupInput::trim($name) === '' || mb_strlen($name, 'UTF-8') > 200) {
                throw CatalogFailure::field('recipientName', "the recipient's name must be 1 to 200 characters");
            }
            $email = null;
            if (($payer['email'] ?? null) === null) {
                $email = $payment['email'] !== '' ? $payment['email'] : ($registration->guest['email'] ?? null);
                if ($email !== null && !SetupInput::validEmail($email)) {
                    throw CatalogFailure::field('recipientEmail', 'invalid email address');
                }
            }

            $now = $this->clock->now();
            $timezone = new DateTimeZone($settings['timezone']);
            $actor = $principal->impersonatorId ?? $principal->accountId;
            $locale = $settings['default_locale'];
            [$first, $last] = self::eventDays($this->events->sessions([$event->id]), $timezone);
            $item = static fn(string $lang): string => self::description($lang, $event->title, $participant, $first, $last);

            $gross = $registration->price;
            $lines = [ReceiptIssuance::saleLine($item(Document::language($locale)), $gross, $registration->vatRate, $settings['tax_mode'])];
            $receipt = $this->receipts->issue($settings, $actor, $now, [
                'kind' => 'sale', 'tax_mode' => $settings['tax_mode'] ?? '', 'tax_note' => $settings['tax_note'],
                'currency' => $settings['currency'], 'gross' => $gross, 'recipient_name' => $name,
                'recipient_address' => $payment['address'] === '' ? null : $payment['address'],
                'payment_method' => $payment['method'], 'payerId' => $payerId, 'recipientEmail' => $email,
            ], $lines, $item);

            $updated = $this->registrations->markPaid($id, $receipt['id'], $now);
            $this->registrations->audit($updated, $actor, 'event-registration-paid', $now);

            return [
                'registration' => EventRegistrationReads::officeDocument($updated),
                'receipt' => $receipt,
            ];
        });
        if ($this->wakeMail !== null) {
            ($this->wakeMail)();
        }

        return $result;
    }

    private static function requireUnpaidPlace(EventRegistration $registration): void
    {
        if ($registration->status === 'canceled') {
            throw new CatalogFailure(409, 'the registration is canceled already', reason: 'already-canceled');
        }
        if ($registration->status !== 'registered') {
            throw new CatalogFailure(409, 'the registration is on the waiting list', reason: 'not-registered');
        }
        if ($registration->payment !== 'unpaid') {
            throw new CatalogFailure(409, 'the registration is paid already', reason: 'already-paid');
        }
    }

    /**
     * @param list<PublishedLesson> $sessions
     * @return array{?string,?string}
     */
    private static function eventDays(array $sessions, DateTimeZone $timezone): array
    {
        $scheduled = array_values(array_filter($sessions, static fn(PublishedLesson $session): bool => $session->status === 'scheduled'));
        $days = $scheduled === [] ? $sessions : $scheduled;
        if ($days === []) {
            return [null, null];
        }

        return [
            $days[0]->startsAt->setTimezone($timezone)->format('Y-m-d'),
            $days[array_key_last($days)]->startsAt->setTimezone($timezone)->format('Y-m-d'),
        ];
    }

    private static function description(string $language, string $title, string $participant, ?string $first, ?string $last): string
    {
        $description = $title . ($language === 'de' ? ' für ' : ' for ') . $participant;
        if ($first === null) {
            return $description;
        }
        $description .= ', ' . Document::formatDate($first, $language);

        return $last === null || $last === $first ? $description : $description . ' – ' . Document::formatDate($last, $language);
    }

    private static function notFound(): CatalogFailure
    {
        return new CatalogFailure(404, 'resource not found');
    }
}
