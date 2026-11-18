<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Log\NullLogger;
use RuntimeException;
use StudioSchedule\Mail\Mailer;
use StudioSchedule\Receipt\AttachmentStore;
use StudioSchedule\Receipt\Attachments;
use StudioSchedule\Receipt\Renderer;
use StudioSchedule\Service\BackgroundJobs;
use StudioSchedule\Service\JobResult;
use StudioSchedule\Service\SchoolSetup;
use StudioSchedule\Service\SetupInput;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;
use StudioSchedule\Store\SetupStore;

/** @phpstan-import-type ReceiptDocument from Renderer */
final class ReceiptAttachmentsTest extends TestCase
{
    private const string SALE = '018f0000-0000-7000-8000-000000000001';
    private const string CANCELLATION = '018f0000-0000-7000-8000-000000000002';
    private const string RACING = '018f0000-0000-7000-8000-000000000003';
    private const string NOW = '2026-10-21 12:00:00';

    public function testMailRendersFromTheSnapshotAndKeepsTheFirstStoredPdf(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database provisioned by just php-test');
        }

        $db = Database::connect($dsn);
        $postgres = $db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        self::assertSame([], (new Schema($db))->relations(), 'Receipt tests require an empty disposable database');
        $migrations = $postgres
            ? (getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations')
            : dirname(__DIR__) . '/migrations/mysql';
        try {
            (new Migrator($dsn))->up($migrations);
            $clock = self::createStub(ClockInterface::class);
            $clock->method('now')->willReturn(new DateTimeImmutable(self::NOW . ' UTC'));
            (new SchoolSetup(new SetupStore($db), $clock))->init(
                new SetupInput('Current School Name', 'Pacific/Auckland', 'en', 'USD', 'admin@example.test', 'Ada', 'Example'),
            );
            $this->insertReceipt($db, self::SALE, 1, null);
            $this->insertReceipt($db, self::CANCELLATION, 2, self::SALE);
            $this->insertReceipt($db, self::RACING, 3, null);
            $store = new AttachmentStore($db);

            $snapshot = file_get_contents(__DIR__ . '/fixtures/receipts/de-standard-cancellation.json');
            self::assertIsString($snapshot);
            $expected = json_decode($snapshot, true, flags: JSON_THROW_ON_ERROR);
            self::assertIsArray($expected);
            // mPDF gets the logo bytes separately and prints the date from meta.
            unset($expected['date'], $expected['logo']);
            $calls = 0;
            $renderer = new Renderer();
            $attachments = new Attachments($store, static function (array $document, string $logo) use ($db, &$calls, $expected, $renderer): string {
                self::assertFalse($db->isTransactionActive());
                self::assertSame('', $logo);
                self::assertEquals($expected, $document);
                ++$calls;

                return $renderer->render($document, $logo);
            });
            self::assertNull($attachments->find('invalid-id'));
            self::assertNull($attachments->find('018f0000-0000-7000-8000-000000000099'));
            $file = $attachments->find(self::CANCELLATION);
            self::assertNotNull($file);
            self::assertSame('SD-2026-0002.pdf', $file['filename']);
            self::assertStringStartsWith('%PDF-', $file['pdf']);
            self::assertSame($file, $attachments->find(self::CANCELLATION));
            self::assertSame(1, $calls);

            $failure = new Attachments($store, static function (): never {
                throw new RuntimeException('Renderer unavailable');
            });
            try {
                $failure->find(self::RACING);
                self::fail('Expected the render to fail');
            } catch (RuntimeException $error) {
                self::assertSame('Renderer unavailable', $error->getMessage());
            }
            self::assertNull($store->find(self::RACING));
            $winner = "%PDF-1.7\r\n\x00\x80\xff";
            $raced = new Attachments($store, function () use ($dsn, $winner): string {
                $other = Database::connect($dsn, dedicated: true);
                try {
                    $other->executeStatement(
                        'INSERT INTO receipt_pdfs (receipt_id, pdf) VALUES (?, ?)',
                        [$this->id($other, self::RACING), $winner],
                        [ParameterType::STRING, ParameterType::BINARY],
                    );
                } finally {
                    $other->close();
                }

                return '%PDF-losing-render';
            });
            self::assertSame(['filename' => 'SD-2026-0003.pdf', 'pdf' => $winner], $raced->find(self::RACING));

            $this->assertMailAttachment($db, $dsn, $clock, $store);
        } finally {
            try {
                if (!$postgres) {
                    $db->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
                }
                foreach ($db->createSchemaManager()->listTableNames() as $table) {
                    $db->executeStatement('DROP TABLE ' . $db->quoteSingleIdentifier($table) . ($postgres ? ' CASCADE' : ''));
                }
                if ($postgres) {
                    $db->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
                }
            } finally {
                $db->close();
            }
        }
    }

    private function assertMailAttachment(Connection $db, string $dsn, ClockInterface $clock, AttachmentStore $store): void
    {
        $image = imagecreatetruecolor(40, 20);
        self::assertNotFalse($image);
        $stream = fopen('php://memory', 'w+b');
        self::assertIsResource($stream);
        try {
            self::assertTrue(imagepng($image, $stream));
            rewind($stream);
            $logo = stream_get_contents($stream);
            self::assertIsString($logo);
        } finally {
            fclose($stream);
        }
        $db->executeStatement('UPDATE settings SET logo = ?, logo_type = ?, logo_digest = ?', [
            $logo, 'image/png', hash('sha256', "image/png\n" . $logo),
        ], [ParameterType::BINARY, ParameterType::STRING, ParameterType::STRING]);
        $db->insert('outbox_mails', [
            'id' => $this->id($db, '018f0000-0000-7000-8000-000000000011'),
            'recipient' => 'receipt@example.test',
            'subject' => 'Receipt fixture',
            'body_text' => 'The PDF is attached.',
            'attempts' => 0,
            'receipt_id' => $this->id($db, self::SALE),
            'created_at' => self::NOW,
            'next_attempt_at' => self::NOW,
        ]);
        $mailer = new class implements Mailer {
            /** @var list<array{string,?string,?string}> */
            public array $sent = [];

            public function send(string $to, string $subject, string $text, string $html, ?string $replyTo = null, ?string $attachmentName = null, ?string $attachmentBytes = null): void
            {
                $this->sent[] = [$to, $attachmentName, $attachmentBytes];
            }
        };
        $jobs = new BackgroundJobs($db, $dsn, $clock, new NullLogger());
        self::assertSame(JobResult::Ok, $jobs->deliverMail($mailer));
        self::assertCount(1, $mailer->sent);
        [$to, $name, $pdf] = $mailer->sent[0];
        self::assertSame('receipt@example.test', $to);
        self::assertSame('SD-2026-0001.pdf', $name);
        self::assertNotNull($pdf);
        self::assertStringStartsWith('%PDF-', $pdf);
        self::assertMatchesRegularExpression('~/Subtype\s*/Image\b~', $pdf);
        self::assertSame(['filename' => $name, 'pdf' => $pdf], $store->find(self::SALE));
        self::assertNotNull($db->fetchOne('SELECT sent_at FROM outbox_mails'));
    }

    private function insertReceipt(Connection $db, string $uuid, int $sequence, ?string $original): void
    {
        $id = $this->id($db, $uuid);
        $cancellation = $original !== null;
        $sign = $cancellation ? -1 : 1;
        $db->insert('receipts', [
            'id' => $id,
            'number' => sprintf('SD-2026-%04d', $sequence),
            'year' => 2026,
            'sequence' => $sequence,
            'kind' => $cancellation ? 'cancellation' : 'sale',
            'cancels_receipt_id' => $original === null ? null : $this->id($db, $original),
            'issued_at' => self::NOW,
            'issue_date' => '2026-10-21',
            'locale' => 'de-AT',
            'issuer_name' => 'Studio Demo Dance School',
            'issuer_address' => "Pirouette Lane 7\r\n \n\u{00a0}10999 Exampletown ",
            'issuer_tax_id' => '99/999/99999',
            'issuer_email' => 'office@studio-demo.example',
            'issuer_phone' => '+49 000 0000000',
            'tax_mode' => 'standard',
            'footer' => 'Bank: Example Bank · IBAN XX00 0000 0000 0000 0000 00',
            'recipient_name' => 'Anna Example',
            'payment_method' => 'transfer',
            'currency' => 'EUR',
            'net' => $sign * 10084,
            'vat' => $sign * 1916,
            'gross' => $sign * 12000,
            'reason' => $cancellation ? 'Doppelt verkauft' : null,
        ]);
        $db->insert('receipt_lines', [
            'receipt_id' => $id,
            'position' => 1,
            'description' => '10-visit pass für Mia Example, gültig 21.10.2026 – 18.04.2027',
            'quantity' => 1,
            'unit_price' => $sign * 12000,
            'vat_rate_bp' => 1900,
            'net' => $sign * 10084,
            'vat' => $sign * 1916,
            'gross' => $sign * 12000,
        ]);
    }

    private function id(Connection $db, string $uuid): string
    {
        return $db->getDatabasePlatform() instanceof PostgreSQLPlatform ? $uuid : pack('H*', str_replace('-', '', $uuid));
    }
}
