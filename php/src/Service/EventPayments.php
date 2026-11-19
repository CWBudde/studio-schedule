<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\EventRegistration;
use StudioSchedule\Domain\Money;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Mail\Renderer as MailRenderer;
use StudioSchedule\Receipt\Document;
use StudioSchedule\Receipt\Renderer;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type Settings from ReceiptWritePersistence
 * @phpstan-import-type ReceiptDocument from Renderer
 * @phpstan-type Payment array{method:'cash'|'transfer'|'card',payerId:?string,name:string,address:string,email:string}
 */
final readonly class EventPayments
{
    /** @param (Closure(ReceiptDocument,string):string)|null $render */
    public function __construct(
        private EventPersistence $events,
        private EventRegistrationPersistence $registrations,
        private ReceiptWritePersistence $receipts,
        private ClockInterface $clock,
        private MailRenderer $mail,
        private ?Closure $wakeMail = null,
        private ?Closure $render = null,
    ) {}

    /**
     * @param Payment $payment
     * @return array{registration:array<string,mixed>,receipt:array{id:string,number:string,issueDate:string}}
     */
    public function pay(SessionPrincipal $principal, string $id, #[SensitiveParameter] array $payment): array
    {
        $result = $this->events->write(function () use ($principal, $id, $payment): array {
            $settings = $this->receipts->settings() ?? throw self::notFound();
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
            self::requireIssuer($settings);

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
            $day = $now->setTimezone($timezone);
            $year = (int) $day->format('Y');
            $issueDate = $day->format('Y-m-d');
            $sequence = $this->receipts->nextSequence($year);
            $number = sprintf('%s-%04d-%04d', $settings['receipt_prefix'], $year, $sequence);
            $receiptId = Uuid::v7()->toRfc4122();
            $actor = $principal->impersonatorId ?? $principal->accountId;
            $locale = $settings['default_locale'];
            [$first, $last] = self::eventDays($this->events->sessions([$event->id]), $timezone);
            $item = static fn(string $lang): string => self::description($lang, $event->title, $participant, $first, $last);

            $gross = $registration->price;
            $rate = $settings['tax_mode'] === 'standard' ? $registration->vatRate : 0;
            $denominator = 10000 + $rate;
            $net = intdiv($gross * 10000 + intdiv($denominator, 2), $denominator);
            $lines = [[
                'position' => 1, 'description' => $item(Document::language($locale)), 'quantity' => 1, 'unit_price' => $gross,
                'vat_rate_bp' => $rate, 'net' => $net, 'vat' => $gross - $net, 'gross' => $gross,
            ]];
            $header = [
                'number' => $number, 'kind' => 'sale', 'issue_date' => $issueDate, 'locale' => $locale,
                'issuer_name' => $settings['issuer_name'] ?? '', 'issuer_address' => $settings['issuer_address'] ?? '',
                'issuer_tax_id' => $settings['issuer_tax_id'] ?? '', 'issuer_email' => $settings['issuer_email'],
                'issuer_phone' => $settings['issuer_phone'], 'tax_mode' => $settings['tax_mode'] ?? '', 'tax_note' => $settings['tax_note'],
                'footer' => $settings['receipt_footer'], 'recipient_name' => $name,
                'recipient_address' => $payment['address'] === '' ? null : $payment['address'],
                'payment_method' => $payment['method'], 'currency' => $settings['currency'], 'gross' => $gross,
                'reason' => null, 'original_number' => null, 'original_date' => null,
            ];
            $this->receipts->record($receiptId, $sequence, $now, $actor, $payerId, $email, $header, $lines);
            $render = $this->render ?? (new Renderer())->render(...);
            $this->receipts->putPdf($receiptId, $render(Document::fromSnapshot($header, $lines), $this->receipts->logo()));
            $to = $payer['email'] ?? $email;
            if ($to !== null) {
                if (!SetupInput::validEmail($to)) {
                    throw new RuntimeException('The receipt payer has an invalid account email');
                }
                $payerLocale = $payer['locale'] ?? '';
                $mailLocale = $payerLocale === '' ? $locale : $payerLocale;
                $lang = Document::language($mailLocale);
                $message = $this->mail->render($mailLocale, 'receipt', [
                    'SchoolName' => $settings['school_name'], 'GivenName' => $payer !== null && $payer['email'] !== null ? $payer['given_name'] : '',
                    'Number' => $number, 'Cancellation' => false, 'Cancels' => '',
                    'Amount' => (new Money($gross))->format($settings['currency'], $lang), 'Item' => $item($lang),
                ]);
                $this->receipts->enqueue($receiptId, $to, $message, $now);
            }

            $updated = $this->registrations->markPaid($id, $receiptId, $now);
            $this->registrations->audit($updated, $actor, 'event-registration-paid', $now);

            return [
                'registration' => EventRegistrationReads::officeDocument($updated),
                'receipt' => ['id' => $receiptId, 'number' => $number, 'issueDate' => $issueDate],
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

    /** @param Settings $settings */
    private static function requireIssuer(array $settings): void
    {
        $missing = match (true) {
            SetupInput::trim($settings['issuer_name'] ?? '') === '' => "the issuer's name is missing",
            SetupInput::trim($settings['issuer_address'] ?? '') === '' => "the issuer's address is missing",
            SetupInput::trim($settings['issuer_tax_id'] ?? '') === '' => 'the tax number or VAT ID is missing',
            !in_array($settings['tax_mode'], ['standard', 'small_business', 'exempt'], true) => 'the tax mode is missing',
            $settings['tax_mode'] === 'exempt' && SetupInput::trim($settings['tax_note'] ?? '') === ''
                => 'the exempt tax mode needs a note with its legal basis',
            default => null,
        };
        if ($missing !== null) {
            throw new CatalogFailure(409, "the receipt settings are incomplete: $missing", reason: 'receipt-settings-incomplete');
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
