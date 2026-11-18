<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use InvalidArgumentException;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Receipt\Renderer;

/** @phpstan-import-type ReceiptDocument from Renderer */
final class ReceiptRendererTest extends TestCase
{
    private const string FIXTURES = __DIR__ . '/fixtures/receipts';

    #[DataProvider('goldenCases')]
    public function testRendersTheGoReceiptTextInOrder(string $fixture, string $receipt, string $logoType): void
    {
        $document = self::document($fixture);
        if ($receipt === 'event') {
            $lines = $document['lines'] ?? [];
            $lines[0] = ['description' => 'Contemporary-Intensivkurs für Lea Öztürk, 24.10.2026–25.10.2026']
                + ($lines[0] ?? self::fail('The Go receipt sample has no line item'));
            $document['lines'] = $lines;
        }
        $logo = $logoType === '' ? '' : self::logo($logoType);
        $pdf = (new Renderer())->render($document, $logo);

        self::assertStringStartsWith('%PDF-', $pdf);
        $expected = file_get_contents(self::FIXTURES . '/' . $receipt . '.txt');
        self::assertIsString($expected);
        self::assertSame(self::normalize($expected), self::normalize(self::bodyText($pdf) . ' ' . self::footerText($pdf)));

        $image = '~/Subtype\s*/Image\b~';
        if ($logo === '') {
            self::assertDoesNotMatchRegularExpression($image, $pdf);
        } else {
            self::assertMatchesRegularExpression($image, $pdf);
        }
    }

    /** @return iterable<string, array{string, string, string}> */
    public static function goldenCases(): iterable
    {
        // The sample and event line use the three checked-in checksums in
        // internal/pdf/typstwasm. The remaining cases use the actual template
        // inputs and Typst output from the receipt service's real-PDF tests.
        yield 'receipt with PNG' => ['sample', 'sale', 'png'];
        yield 'receipt with JPEG' => ['sample', 'sale', 'jpeg'];
        yield 'receipt without logo' => ['sample', 'sale', ''];
        yield 'event receipt' => ['sample', 'event', 'png'];
        foreach (['de-standard', 'en-standard', 'en-small-business', 'en-exempt'] as $tax) {
            foreach (['sale', 'cancellation'] as $kind) {
                $name = $tax . '-' . $kind;
                yield $name => [$name, $name, ''];
            }
        }
    }

    public function testEmbedsTheReceiptFontAndMetadata(): void
    {
        $pdf = (new Renderer())->render(self::document('sample'));
        $metadata = self::readPdf(['pdfinfo', '-'], $pdf);
        self::assertMatchesRegularExpression('/^Title:\s+Quittung SD-2026-0001$/m', $metadata);
        self::assertMatchesRegularExpression('/^Author:\s+Tanzstudio Beispiel$/m', $metadata);
        self::assertMatchesRegularExpression('/^Page size:\s+595\.\d+ x 841\.\d+ pts \(A4\)$/m', $metadata);
        $fonts = self::readPdf(['pdffonts', '-'], $pdf);
        self::assertMatchesRegularExpression('/LibertinusSerif-Regular\s+TrueType\s+\S+\s+yes/', $fonts);
        self::assertMatchesRegularExpression('/LibertinusSerif-Bold\s+TrueType\s+\S+\s+yes/', $fonts);
    }

    public function testRepeatsTheFooterAndNumbersMultiplePages(): void
    {
        $document = self::document('sample');
        $lines = $document['lines'] ?? [];
        self::assertNotEmpty($lines);
        $line = $lines[0];
        $document['lines'] = [];
        for ($i = 1; $i <= 85; $i++) {
            $document['lines'][] = ['pos' => (string) $i] + $line;
        }

        $pdf = (new Renderer())->render($document);
        $metadata = self::readPdf(['pdfinfo', '-'], $pdf);
        if (preg_match('/^Pages:\s+(\d+)$/m', $metadata, $matches) !== 1) {
            self::fail('The PDF does not have a page count');
        }
        $pages = (int) $matches[1];
        self::assertGreaterThan(1, $pages);

        $text = self::normalize(self::footerText($pdf));
        self::assertSame($pages, substr_count($text, 'Steuernummer 12/345/67890'));
        for ($page = 1; $page <= $pages; $page++) {
            self::assertStringContainsString("$page / $pages", $text);
        }
        self::assertStringContainsString('85 10er-Karte', self::normalize(self::bodyText($pdf)));
    }

    public function testReceiptFieldsAreRenderedAsText(): void
    {
        $document = self::document('sample');
        $document['recipient'] = ['<img src="https://example.invalid/secret.png"> & Äva'];
        $document['notes'] = null;
        $document['footer'] = null;
        $pdf = (new Renderer())->render($document);

        self::assertStringContainsString('<img src="https://example.invalid/secret.png"> & Äva', self::normalize(self::bodyText($pdf)));
        self::assertDoesNotMatchRegularExpression('~/Subtype\s*/Image\b~', $pdf);
    }

    public function testRejectsANonImageLogo(): void
    {
        $this->expectException(InvalidArgumentException::class);
        $this->expectExceptionMessage('Receipt logo must be a PNG or JPEG image');
        (new Renderer())->render(self::document('sample'), 'https://example.invalid/logo.png');
    }

    /** @return ReceiptDocument */
    private static function document(string $fixture): array
    {
        $source = getenv('STUDIO_PHP_TEST_RECEIPT_GOLDENS') ?: dirname(__DIR__, 2) . '/internal/pdf/typstwasm/testdata';
        $path = $fixture === 'sample' ? $source . '/receipt.json' : self::FIXTURES . '/' . $fixture . '.json';
        $json = file_get_contents($path);
        self::assertIsString($json);
        /** @var ReceiptDocument $document */
        $document = json_decode($json, true, flags: JSON_THROW_ON_ERROR);

        return $document;
    }

    private static function logo(string $type): string
    {
        $image = imagecreatetruecolor(40, 20);
        self::assertNotFalse($image);
        $color = imagecolorallocate($image, 180, 80, 40);
        self::assertNotFalse($color);
        imagefill($image, 0, 0, $color);
        $stream = fopen('php://memory', 'w+b');
        self::assertIsResource($stream);
        self::assertTrue($type === 'png' ? imagepng($image, $stream) : imagejpeg($image, $stream));
        rewind($stream);
        $bytes = stream_get_contents($stream);
        fclose($stream);
        self::assertIsString($bytes);

        return $bytes;
    }

    private static function normalize(string $text): string
    {
        // Layout can wrap an uninterrupted date range at the en dash.
        $text = (string) preg_replace('/(\d)–\R(?=\d)/u', '$1–', $text);

        return trim((string) preg_replace('/[\s\p{Z}]+/u', ' ', $text));
    }

    private static function bodyText(string $pdf): string
    {
        return self::readPdf(['pdftotext', '-raw', '-nopgbrk', '-W', '596', '-y', '0', '-H', '760', '-', '-'], $pdf);
    }

    private static function footerText(string $pdf): string
    {
        // mPDF draws the footer layer before the body even though it sits at
        // the bottom of the page. Compare body and footer in reading order.
        return self::readPdf(['pdftotext', '-raw', '-nopgbrk', '-W', '596', '-y', '760', '-H', '90', '-', '-'], $pdf);
    }

    /** @param non-empty-list<string> $tool */
    private static function readPdf(array $tool, string $pdf): string
    {
        $spec = [['pipe', 'r'], ['pipe', 'w'], ['pipe', 'w']];
        $process = proc_open($tool, $spec, $pipes);
        self::assertIsResource($process);
        fwrite($pipes[0], $pdf);
        fclose($pipes[0]);
        $text = stream_get_contents($pipes[1]);
        fclose($pipes[1]);
        $errors = stream_get_contents($pipes[2]);
        fclose($pipes[2]);
        self::assertSame(0, proc_close($process), (string) $errors);
        self::assertIsString($text);

        return $text;
    }
}
