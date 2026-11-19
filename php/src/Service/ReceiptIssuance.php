<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use DateTimeZone;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\Money;
use StudioSchedule\Mail\Renderer as MailRenderer;
use StudioSchedule\Receipt\Document;
use StudioSchedule\Receipt\Renderer;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type Settings from ReceiptWritePersistence
 * @phpstan-import-type Payer from ReceiptWritePersistence
 * @phpstan-import-type ReceiptLine from \StudioSchedule\Receipt\AttachmentStore
 * @phpstan-import-type ReceiptDocument from Renderer
 * @phpstan-type ReceiptRef array{id:string,number:string,issueDate:string}
 * @phpstan-type ReceiptDetails array{
 *     kind:'sale'|'cancellation', tax_mode:string, tax_note:?string, currency:string, gross:int,
 *     recipient_name:string, recipient_address:?string, payment_method:'cash'|'transfer'|'card',
 *     payerId:?string, recipientEmail:?string, originalId?:string, originalNumber?:string, originalDate?:string
 * }
 */
final readonly class ReceiptIssuance
{
    /** @param (Closure(ReceiptDocument,string):string)|null $render */
    public function __construct(private ReceiptWritePersistence $store, private MailRenderer $mail, private ?Closure $render = null) {}

    /** @return Settings */
    public function settings(): array
    {
        return $this->store->settings() ?? throw new CatalogFailure(404, 'resource not found');
    }

    /** @return Payer */
    public function payer(string $id): array
    {
        return $this->store->payer($id);
    }

    /**
     * Issue the receipt in the caller's write transaction.
     *
     * @param Settings $settings
     * @param ReceiptDetails $details
     * @param list<ReceiptLine> $lines
     * @param Closure(string):string $item
     * @return ReceiptRef
     */
    public function issue(
        array $settings,
        string $actor,
        DateTimeImmutable $now,
        #[SensitiveParameter]
        array $details,
        #[SensitiveParameter]
        array $lines,
        Closure $item,
    ): array {
        $day = $now->setTimezone(new DateTimeZone($settings['timezone']));
        $year = (int) $day->format('Y');
        $issueDate = $day->format('Y-m-d');
        $sequence = $this->store->nextSequence($year);
        $number = sprintf('%s-%04d-%04d', $settings['receipt_prefix'], $year, $sequence);
        $id = Uuid::v7()->toRfc4122();
        $locale = $settings['default_locale'];

        $header = [
            'number' => $number, 'kind' => $details['kind'], 'issue_date' => $issueDate, 'locale' => $locale,
            'issuer_name' => $settings['issuer_name'] ?? '', 'issuer_address' => $settings['issuer_address'] ?? '',
            'issuer_tax_id' => $settings['issuer_tax_id'] ?? '', 'issuer_email' => $settings['issuer_email'],
            'issuer_phone' => $settings['issuer_phone'], 'tax_mode' => $details['tax_mode'], 'tax_note' => $details['tax_note'],
            'footer' => $settings['receipt_footer'], 'recipient_name' => $details['recipient_name'],
            'recipient_address' => $details['recipient_address'], 'payment_method' => $details['payment_method'],
            'currency' => $details['currency'], 'gross' => $details['gross'], 'reason' => null,
            'original_number' => $details['originalNumber'] ?? null, 'original_date' => $details['originalDate'] ?? null,
        ];
        $this->store->record(
            $id,
            $sequence,
            $now,
            $actor,
            $details['payerId'],
            $details['recipientEmail'],
            $header,
            $lines,
            cancelsReceiptId: $details['originalId'] ?? null,
        );
        $render = $this->render ?? (new Renderer())->render(...);
        $this->store->putPdf($id, $render(Document::fromSnapshot($header, $lines), $this->store->logo()));

        $payer = $details['payerId'] === null ? null : $this->store->payer($details['payerId']);
        $to = $payer['email'] ?? $details['recipientEmail'];
        if ($to !== null) {
            if (!SetupInput::validEmail($to)) {
                throw new RuntimeException('The receipt has an invalid delivery address');
            }
            $payerLocale = $payer['locale'] ?? '';
            $mailLocale = $payerLocale === '' ? $locale : $payerLocale;
            $lang = Document::language($mailLocale);
            $message = $this->mail->render($mailLocale, 'receipt', [
                'SchoolName' => $settings['school_name'], 'GivenName' => $payer !== null && $payer['email'] !== null ? $payer['given_name'] : '',
                'Number' => $number, 'Cancellation' => $details['kind'] === 'cancellation', 'Cancels' => $details['originalNumber'] ?? '',
                'Amount' => (new Money(abs($details['gross'])))->format($details['currency'], $lang), 'Item' => $item($lang),
            ]);
            $this->store->enqueue($id, $to, $message, $now);
        }

        return ['id' => $id, 'number' => $number, 'issueDate' => $issueDate];
    }

    /**
     * @param Settings $settings
     * @return ReceiptRef
     */
    public function refund(array $settings, string $actor, string $originalId, DateTimeImmutable $now): array
    {
        $original = $this->store->original($originalId) ?? throw new RuntimeException('The fee receipt is missing');
        if ($original['kind'] !== 'sale' || $original['hasCancellation']) {
            throw new CatalogFailure(409, 'conflict');
        }
        self::requireIssuer($settings);

        $lines = [];
        foreach ($original['lines'] as $line) {
            $line['unit_price'] = -(int) $line['unit_price'];
            $line['net'] = -(int) $line['net'];
            $line['vat'] = -(int) $line['vat'];
            $line['gross'] = -(int) $line['gross'];
            $lines[] = $line;
        }
        if ($lines === []) {
            throw new RuntimeException('The fee receipt has no lines');
        }

        return $this->issue($settings, $actor, $now, [
            'kind' => 'cancellation', 'tax_mode' => $original['tax_mode'], 'tax_note' => $original['tax_note'],
            'currency' => $original['currency'], 'gross' => -(int) $original['gross'],
            'recipient_name' => $original['recipient_name'], 'recipient_address' => $original['recipient_address'],
            'payment_method' => $original['payment_method'], 'payerId' => $original['payerId'], 'recipientEmail' => $original['recipient_email'],
            'originalId' => $originalId, 'originalNumber' => $original['number'], 'originalDate' => $original['issue_date'],
        ], $lines, static fn(string $lang): string => $lang === Document::language($original['locale']) ? $lines[0]['description'] : '');
    }

    /** @return ReceiptLine */
    public static function saleLine(string $description, int $gross, int $rate, ?string $taxMode): array
    {
        $rate = $taxMode === 'standard' ? $rate : 0;
        $denominator = 10000 + $rate;
        $net = intdiv($gross * 10000 + intdiv($denominator, 2), $denominator);

        return [
            'position' => 1, 'description' => $description, 'quantity' => 1, 'unit_price' => $gross,
            'vat_rate_bp' => $rate, 'net' => $net, 'vat' => $gross - $net, 'gross' => $gross,
        ];
    }

    /** @param Settings $settings */
    public static function requireIssuer(array $settings): void
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
}
