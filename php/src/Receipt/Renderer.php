<?php

declare(strict_types=1);

namespace StudioSchedule\Receipt;

use InvalidArgumentException;
use Mpdf\Mpdf;
use RecursiveDirectoryIterator;
use RecursiveIteratorIterator;
use RuntimeException;
use SensitiveParameter;
use SplFileInfo;
use Twig\Environment;
use Twig\Loader\FilesystemLoader;

/**
 * @phpstan-type ReceiptDocument array{
 *     title: string,
 *     lang: string,
 *     heading: string,
 *     issuer: array{name: string, lines?: list<string>|null},
 *     recipientLabel: string,
 *     recipient?: list<string>|null,
 *     meta?: list<array{string, string}>|null,
 *     intro?: string|null,
 *     showVat: bool,
 *     columns: array{string, string, string, string, string, string},
 *     lines?: list<array{pos: string, description: string, quantity: string, unit: string, vat: string, total: string}>|null,
 *     summary?: list<array{string, string}>|null,
 *     total: array{string, string},
 *     notes?: list<string>|null,
 *     footer?: list<string>|null
 * }
 */
final readonly class Renderer
{
    private const string FONT = 'libertinusserif';

    private Environment $twig;

    public function __construct()
    {
        $this->twig = new Environment(new FilesystemLoader(dirname(__DIR__, 2) . '/templates/receipt'), [
            'autoescape' => 'html',
            'strict_variables' => true,
        ]);
    }

    /**
     * Render the same preformatted fields as the Go receipt template.
     *
     * @param ReceiptDocument $document
     * @param string $logo Optional PNG or JPEG bytes from receipt settings.
     */
    public function render(#[SensitiveParameter] array $document, #[SensitiveParameter] string $logo = ''): string
    {
        $logoDimensions = self::logoDimensions($logo);
        $template = $this->twig->load('receipt.html.twig');
        $body = $template->renderBlock('body', ['logoDimensions' => $logoDimensions] + $document);

        // mPDF can write intermediate images. Keep them inaccessible to other
        // users of a shared server, and independent of other renders.
        $tempDir = sys_get_temp_dir() . '/studio-schedule-receipt-' . bin2hex(random_bytes(16));
        if (!mkdir($tempDir, 0700)) {
            throw new RuntimeException('Cannot create a temporary receipt directory');
        }

        try {
            $footer = $template->renderBlock('footer', ['numberPages' => false] + $document);
            $pdf = self::renderPages($tempDir, $document, $body, $footer, $logo);

            // Preserve the Typst footer: a one-page receipt has no page number.
            if ($pdf->PageNo() > 1) {
                $footer = $template->renderBlock('footer', ['numberPages' => true] + $document);
                unset($pdf);
                $pdf = self::renderPages($tempDir, $document, $body, $footer, $logo);
            }

            /** @var string $bytes OutputBinaryData uses mPDF's string-return destination. */
            $bytes = $pdf->OutputBinaryData();

            return $bytes;
        } finally {
            self::removeTemporaryDirectory($tempDir);
        }
    }

    /** @param ReceiptDocument $document */
    private static function renderPages(
        string $tempDir,
        #[SensitiveParameter]
        array $document,
        #[SensitiveParameter]
        string $body,
        #[SensitiveParameter]
        string $footer,
        #[SensitiveParameter]
        string $logo,
    ): Mpdf {
        $pdf = new Mpdf([
            'tempDir' => $tempDir,
            'format' => 'A4',
            'margin_top' => 20,
            'margin_right' => 20,
            'margin_bottom' => 28,
            'margin_left' => 22,
            'margin_footer' => 11,
            'default_font' => self::FONT,
            'default_font_size' => 10.5,
            'fontDir' => [dirname(__DIR__, 2) . '/assets/receipt-fonts'],
            'fontdata' => [self::FONT => [
                'R' => 'LibertinusSerif-Regular.ttf',
                'B' => 'LibertinusSerif-Bold.ttf',
                'I' => 'LibertinusSerif-Italic.ttf',
            ]],
            'whitelistStreamWrappers' => [],
            'showImageErrors' => true,
        ]);
        $pdf->SetTitle($document['title']);
        $pdf->SetAuthor($document['issuer']['name']);
        if ($logo !== '') {
            // The template uses mPDF's in-memory image variable. No file
            // name, URL or HTML is supplied by the receipt data.
            $pdf->imageVars = ['receiptLogo' => $logo];
        }
        $pdf->SetHTMLFooter($footer);
        $pdf->WriteHTML($body);

        return $pdf;
    }

    /** @return array{width: float, height: float}|null */
    private static function logoDimensions(#[SensitiveParameter] string $logo): ?array
    {
        if ($logo === '') {
            return null;
        }
        $size = @getimagesizefromstring($logo);
        if ($size === false || !in_array($size[2], [IMAGETYPE_PNG, IMAGETYPE_JPEG], true) || $size[0] <= 0 || $size[1] <= 0) {
            throw new InvalidArgumentException('Receipt logo must be a PNG or JPEG image');
        }
        $scale = min(50 / $size[0], 20 / $size[1]);

        return ['width' => $size[0] * $scale, 'height' => $size[1] * $scale];
    }

    private static function removeTemporaryDirectory(string $directory): void
    {
        $files = new RecursiveIteratorIterator(
            new RecursiveDirectoryIterator($directory, RecursiveDirectoryIterator::SKIP_DOTS),
            RecursiveIteratorIterator::CHILD_FIRST,
        );
        /** @var SplFileInfo $file */
        foreach ($files as $file) {
            if ($file->isDir()) {
                @rmdir($file->getPathname());
            } else {
                @unlink($file->getPathname());
            }
        }
        @rmdir($directory);
    }
}
