<?php

declare(strict_types=1);

namespace StudioSchedule\Receipt;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;

final readonly class AttachmentStore
{
    public function __construct(private Connection $db) {}

    /** @return array{filename: string, pdf: string}|null */
    public function find(string $receiptId): ?array
    {
        if (preg_match('/\A[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}\z/i', $receiptId) !== 1) {
            return null;
        }

        $postgres = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $id = $postgres ? '?' : 'UNHEX(?)';
        /** @var array{string, string|resource}|false $row */
        $row = $this->db->fetchNumeric(
            'SELECT r.number, p.pdf FROM receipt_pdfs p JOIN receipts r ON r.id = p.receipt_id '
            . "WHERE p.receipt_id = $id",
            [$postgres ? $receiptId : str_replace('-', '', $receiptId)],
        );
        if ($row === false) {
            return null;
        }

        [$number, $pdf] = $row;
        // PDO PostgreSQL can return bytea as a stream. PDO MySQL returns a string.
        if (!is_string($pdf)) {
            $stream = $pdf;
            $pdf = stream_get_contents($stream);
            fclose($stream);
            if ($pdf === false) {
                throw new RuntimeException('Could not read the cached receipt PDF');
            }
        }

        return ['filename' => $number . '.pdf', 'pdf' => $pdf];
    }
}
