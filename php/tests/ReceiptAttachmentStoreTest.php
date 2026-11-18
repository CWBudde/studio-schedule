<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Receipt\AttachmentStore;
use StudioSchedule\Store\Database;

final class ReceiptAttachmentStoreTest extends TestCase
{
    public function testReadsTheStoredReceiptNumberAndUnmodifiedPdfBytes(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database provisioned by just php-test');
        }

        $db = Database::connect($dsn, dedicated: true);
        try {
            $postgres = $db->getDatabasePlatform() instanceof PostgreSQLPlatform;
            $idType = $postgres ? 'UUID' : 'BINARY(16)';
            $numberType = $postgres ? 'TEXT' : 'VARBINARY(3072)';
            $pdfType = $postgres ? 'BYTEA' : 'LONGBLOB';
            // Temporary tables use the same types as the production cache and disappear when this connection closes.
            $db->executeStatement("CREATE TEMPORARY TABLE receipts (id $idType PRIMARY KEY, number $numberType NOT NULL)");
            $db->executeStatement("CREATE TEMPORARY TABLE receipt_pdfs (receipt_id $idType PRIMARY KEY, pdf $pdfType NOT NULL)");

            $cachedId = '018f0000-0000-7000-8000-000000000001';
            $uncachedId = '018f0000-0000-7000-8000-000000000002';
            $cachedKey = $postgres ? $cachedId : pack('H*', str_replace('-', '', $cachedId));
            $uncachedKey = $postgres ? $uncachedId : pack('H*', str_replace('-', '', $uncachedId));
            $db->insert('receipts', ['id' => $cachedKey, 'number' => 'SD-2026-0007']);
            $db->insert('receipts', ['id' => $uncachedKey, 'number' => 'SD-2026-0008']);
            $pdf = "%PDF-1.7\r\n\x00\x80\xff\\\r\n%%EOF\n";
            $binary = $postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
            $db->executeStatement("INSERT INTO receipt_pdfs (receipt_id, pdf) VALUES (?, $binary)", [$cachedKey, bin2hex($pdf)]);

            $store = new AttachmentStore($db);
            self::assertSame(['filename' => 'SD-2026-0007.pdf', 'pdf' => $pdf], $store->find($cachedId));
            self::assertNull($store->find($uncachedId));
            self::assertNull($store->find('018f0000-0000-7000-8000-000000000003'));
            self::assertNull($store->find('invalid-id'));
            self::assertSame(['SD-2026-0007', 'SD-2026-0008'], $db->fetchFirstColumn('SELECT number FROM receipts ORDER BY id'));
            $encode = $postgres ? "encode(pdf, 'hex')" : 'LOWER(HEX(pdf))';
            self::assertSame([bin2hex($pdf)], $db->fetchFirstColumn("SELECT $encode FROM receipt_pdfs"));
        } finally {
            $db->close();
        }
    }
}
