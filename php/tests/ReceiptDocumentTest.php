<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Receipt\AttachmentStore;
use StudioSchedule\Receipt\Document;

/**
 * @phpstan-import-type ReceiptRow from AttachmentStore
 * @phpstan-import-type ReceiptLine from AttachmentStore
 */
final class ReceiptDocumentTest extends TestCase
{
    public function testVatSummaryGroupsAndSortsRatesAndTheDateUsesTheStoredLocale(): void
    {
        $document = Document::fromSnapshot(self::receipt(), [
            self::line(1, 1900, 10000, 1900),
            self::line(2, 0, 4900, 0),
            self::line(3, 750, 20000, 1500),
            self::line(4, 1900, 20000, 3800),
        ]);
        self::assertSame('21 Oct 2026', $document['meta'][1][1] ?? null);
        self::assertSame(['Anna Example', 'One Example Road', 'Exampletown'], $document['recipient'] ?? null);
        self::assertSame(['Paid by card.'], $document['notes'] ?? null);
        self::assertSame(['Snapshot School', 'Street 1', 'Tax number 000'], $document['footer'] ?? null);
        self::assertSame([
            ['Without VAT', '€49.00'],
            ['Net (7.5% VAT)', '€200.00'],
            ['VAT 7.5%', '€15.00'],
            ['Net (19% VAT)', '€300.00'],
            ['VAT 19%', '€57.00'],
        ], $document['summary'] ?? null);
        self::assertSame(['19%', '0%', '7.5%', '19%'], array_column($document['lines'] ?? [], 'vat'));
        self::assertSame(['Total', '€621.00'], $document['total']);

        $german = Document::fromSnapshot(['locale' => 'DE-AT'] + self::receipt(), [self::line(1, 705, 10000, 705)]);
        self::assertSame('21.10.2026', $german['meta'][1][1] ?? null);
        self::assertSame(['7,05 %'], array_column($german['lines'] ?? [], 'vat'));
    }

    #[DataProvider('currencies')]
    public function testUsesTheStoredCurrencyAndExemptReceiptsShowTheTaxNote(string $currency, int $minor, string $formatted): void
    {
        $document = Document::fromSnapshot([
            'currency' => $currency, 'gross' => $minor, 'tax_mode' => 'exempt', 'tax_note' => 'Exempt under the fixture rule',
        ] + self::receipt(), [self::line(1, 0, $minor, 0)]);
        self::assertFalse($document['showVat']);
        self::assertSame([], $document['summary'] ?? null);
        self::assertSame([$formatted], array_column($document['lines'] ?? [], 'unit'));
        self::assertSame(['Total', $formatted], $document['total']);
        self::assertSame(['Exempt under the fixture rule', 'Paid by card.'], $document['notes'] ?? null);
    }

    /** @return iterable<array{string, int, string}> */
    public static function currencies(): iterable
    {
        yield ['JPY', 1234, 'JPY 1,234'];
        yield ['KWD', 12, 'KWD 0.012'];
        yield ['CLF', 12345678, 'CLF 1,234.5678'];
        yield ['GBP', PHP_INT_MAX, '£92,233,720,368,547,758.07'];
        yield ['USD', -1200, '−$12.00'];
        yield ['ZZZ', 9, 'ZZZ 0.09'];
    }

    public function testSmallBusinessTextUsesTheStoredLanguageAndAllowsAnOverride(): void
    {
        $german = Document::fromSnapshot(['locale' => 'de', 'tax_mode' => 'small_business'] + self::receipt(), [self::line(1, 0, 62100, 0)]);
        self::assertFalse($german['showVat']);
        self::assertSame(['Gemäß § 19 UStG wird keine Umsatzsteuer berechnet.', 'Mit Karte bezahlt.'], $german['notes'] ?? null);

        $english = Document::fromSnapshot(['tax_mode' => 'small_business'] + self::receipt(), []);
        self::assertSame(['No VAT is charged under the small business rule (§ 19 UStG).', 'Paid by card.'], $english['notes'] ?? null);

        $override = Document::fromSnapshot(['tax_mode' => 'small_business', 'tax_note' => 'Custom notice'] + self::receipt(), []);
        self::assertSame(['Custom notice', 'Paid by card.'], $override['notes'] ?? null);
    }

    /** @return ReceiptRow */
    private static function receipt(): array
    {
        return [
            'number' => 'R-2026-0001', 'kind' => 'sale', 'issue_date' => '2026-10-21', 'locale' => 'fr-CA',
            'issuer_name' => 'Snapshot School', 'issuer_address' => 'Street 1', 'issuer_tax_id' => '000',
            'issuer_email' => null, 'issuer_phone' => null, 'tax_mode' => 'standard', 'tax_note' => null, 'footer' => null,
            'recipient_name' => 'Anna Example', 'recipient_address' => "One Example Road\n \nExampletown", 'payment_method' => 'card',
            'currency' => 'EUR', 'gross' => 62100, 'reason' => null, 'original_number' => null, 'original_date' => null,
        ];
    }

    /** @return ReceiptLine */
    private static function line(int $position, int $rate, int $net, int $vat): array
    {
        return [
            'position' => $position, 'description' => 'Fixture sale', 'quantity' => 1,
            'unit_price' => $net + $vat, 'vat_rate_bp' => $rate, 'net' => $net, 'vat' => $vat, 'gross' => $net + $vat,
        ];
    }
}
