<?php

declare(strict_types=1);

namespace StudioSchedule\Receipt;

use DateTimeImmutable;
use DateTimeZone;
use SensitiveParameter;
use StudioSchedule\Domain\Money;
use StudioSchedule\Service\SetupInput;

/**
 * Match the words and minor-unit formatting in internal/service/receipt_pdf.go.
 *
 * @phpstan-import-type ReceiptDocument from Renderer
 * @phpstan-import-type ReceiptRow from AttachmentStore
 * @phpstan-import-type ReceiptLine from AttachmentStore
 */
final class Document
{
    private const string SPACE = "\u{00a0}";

    private const array LABELS = [
        'en' => [
            'heading' => ['sale' => 'Receipt', 'cancellation' => 'Cancellation'],
            'recipient' => ['sale' => 'Payment received from', 'cancellation' => 'Refunded to'],
            'number' => 'Number', 'date' => 'Date', 'payment' => 'Payment', 'cancels' => 'Cancels', 'of' => 'of',
            'intro' => 'We confirm receipt of the following payment:',
            'cancellation' => 'This cancels receipt %s of %s:',
            'columns' => ['Item', 'Description', 'Qty', 'Unit price', 'VAT', 'Amount'],
            'net' => 'Net (%s VAT)', 'noVat' => 'Without VAT', 'vat' => 'VAT %s', 'total' => 'Total',
            'methods' => ['cash' => 'Cash', 'transfer' => 'Bank transfer', 'card' => 'Card'],
            'sale' => ['cash' => 'Paid in cash.', 'transfer' => 'Paid by bank transfer.', 'card' => 'Paid by card.'],
            'refund' => ['cash' => 'Refunded in cash.', 'transfer' => 'Refunded by bank transfer.', 'card' => 'Refunded to the card.'],
            'smallBusiness' => 'No VAT is charged under the small business rule (§ 19 UStG).',
            'taxID' => 'Tax number', 'email' => 'Email', 'phone' => 'Phone', 'reason' => 'Reason: %s',
        ],
        'de' => [
            'heading' => ['sale' => 'Quittung', 'cancellation' => 'Stornobeleg'],
            'recipient' => ['sale' => 'Zahlung erhalten von', 'cancellation' => 'Erstattet an'],
            'number' => 'Nummer', 'date' => 'Datum', 'payment' => 'Zahlungsart', 'cancels' => 'Storno zu', 'of' => 'vom',
            'intro' => 'Wir bestätigen den Erhalt folgender Zahlung:',
            'cancellation' => 'Hiermit stornieren wir die Quittung %s vom %s:',
            'columns' => ['Pos.', 'Beschreibung', 'Menge', 'Einzelpreis', 'USt.', 'Betrag'],
            'net' => 'Nettobetrag (%s USt.)', 'noVat' => 'Ohne USt.', 'vat' => 'Umsatzsteuer %s', 'total' => 'Gesamtbetrag',
            'methods' => ['cash' => 'Bar', 'transfer' => 'Überweisung', 'card' => 'Karte'],
            'sale' => ['cash' => 'Betrag bar erhalten.', 'transfer' => 'Per Überweisung bezahlt.', 'card' => 'Mit Karte bezahlt.'],
            'refund' => ['cash' => 'Betrag bar erstattet.', 'transfer' => 'Per Überweisung erstattet.', 'card' => 'Auf die Karte erstattet.'],
            'smallBusiness' => 'Gemäß § 19 UStG wird keine Umsatzsteuer berechnet.',
            'taxID' => 'Steuernummer', 'email' => 'E-Mail', 'phone' => 'Tel.', 'reason' => 'Grund: %s',
        ],
    ];

    /**
     * @param ReceiptRow $receipt
     * @param list<ReceiptLine> $lines
     * @return ReceiptDocument
     */
    public static function fromSnapshot(#[SensitiveParameter] array $receipt, #[SensitiveParameter] array $lines): array
    {
        $lang = self::language($receipt['locale']);
        $labels = self::LABELS[$lang];
        $kind = $receipt['kind'];
        $method = $receipt['payment_method'];
        $heading = $labels['heading'][$kind];
        $showVat = $receipt['tax_mode'] === 'standard';
        $currency = $receipt['currency'];
        $money = static fn(int $amount): string => (new Money($amount))->format($currency, $lang);
        $meta = [
            [$labels['number'], $receipt['number']],
            [$labels['date'], self::formatDate($receipt['issue_date'], $lang)],
            [$labels['payment'], $labels['methods'][$method]],
        ];
        $intro = $labels['intro'];
        if ($receipt['original_number'] !== null && $receipt['original_date'] !== null) {
            $number = $receipt['original_number'];
            $date = self::formatDate($receipt['original_date'], $lang);
            $intro = sprintf($labels['cancellation'], $number, $date);
            $meta[] = [$labels['cancels'], $number . ' ' . $labels['of'] . ' ' . $date];
        }

        $items = [];
        $tax = [];
        foreach ($lines as $line) {
            $rate = (int) $line['vat_rate_bp'];
            $items[] = [
                'pos' => (string) $line['position'],
                'description' => $line['description'],
                'quantity' => (string) $line['quantity'],
                'unit' => $money((int) $line['unit_price']),
                'vat' => self::rate($rate, $lang === 'de'),
                'total' => $money((int) $line['gross']),
            ];
            $tax[$rate] ??= [0, 0];
            $tax[$rate][0] += (int) $line['net'];
            $tax[$rate][1] += (int) $line['vat'];
        }

        $summary = [];
        if ($showVat) {
            ksort($tax, SORT_NUMERIC);
            foreach ($tax as $rate => [$net, $vat]) {
                if ($rate === 0) {
                    $summary[] = [$labels['noVat'], $money($net)];
                } else {
                    $formatted = self::rate($rate, $lang === 'de');
                    $summary[] = [sprintf($labels['net'], $formatted), $money($net)];
                    $summary[] = [sprintf($labels['vat'], $formatted), $money($vat)];
                }
            }
        }

        $notes = [];
        if ($receipt['tax_note'] !== null) {
            $notes[] = $receipt['tax_note'];
        } elseif ($receipt['tax_mode'] === 'small_business') {
            $notes[] = $labels['smallBusiness'];
        }
        $notes[] = $kind === 'cancellation' ? $labels['refund'][$method] : $labels['sale'][$method];
        if ($kind === 'cancellation' && $receipt['reason'] !== null) {
            $notes[] = sprintf($labels['reason'], $receipt['reason']);
        }

        $address = self::lines($receipt['issuer_address']);
        $footer = [$receipt['issuer_name'], implode(', ', $address), $labels['taxID'] . ' ' . $receipt['issuer_tax_id']];
        if ($receipt['issuer_email'] !== null) {
            $footer[] = $labels['email'] . ' ' . $receipt['issuer_email'];
        }
        if ($receipt['issuer_phone'] !== null) {
            $footer[] = $labels['phone'] . ' ' . $receipt['issuer_phone'];
        }

        return [
            'title' => $heading . ' ' . $receipt['number'],
            'lang' => $lang,
            'heading' => $heading,
            'issuer' => ['name' => $receipt['issuer_name'], 'lines' => $address],
            'recipientLabel' => $labels['recipient'][$kind],
            'recipient' => [$receipt['recipient_name'], ...self::lines($receipt['recipient_address'] ?? '')],
            'meta' => $meta,
            'intro' => $intro,
            'showVat' => $showVat,
            'columns' => $labels['columns'],
            'lines' => $items,
            'summary' => $summary,
            'total' => [$labels['total'], $money((int) $receipt['gross'])],
            'notes' => $notes,
            'footer' => [...$footer, ...self::lines($receipt['footer'] ?? '')],
        ];
    }

    public static function language(string $locale): string
    {
        return explode('-', strtolower($locale), 2)[0] === 'de' ? 'de' : 'en';
    }

    public static function formatDate(string $iso, string $lang): string
    {
        $date = new DateTimeImmutable($iso, new DateTimeZone('UTC'));

        return $lang === 'de' ? $date->format('d.m.Y') : $date->format('j M ') . (int) $date->format('Y');
    }

    private static function rate(int $basisPoints, bool $german): string
    {
        $whole = (string) intdiv($basisPoints, 100);
        $fraction = $basisPoints % 100;
        if ($fraction !== 0) {
            $whole .= ($german ? ',' : '.') . rtrim(sprintf('%02d', $fraction), '0');
        }

        return $whole . ($german ? self::SPACE : '') . '%';
    }

    /** @return list<string> */
    private static function lines(string $value): array
    {
        return array_values(array_filter(array_map(SetupInput::trim(...), explode("\n", $value)), static fn(string $line): bool => $line !== ''));
    }
}
