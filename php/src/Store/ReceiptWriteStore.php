<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\BrandingImage;
use StudioSchedule\Mail\Rendered;
use StudioSchedule\Receipt\AttachmentStore;
use StudioSchedule\Service\ReceiptWritePersistence;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type Settings from ReceiptWritePersistence
 * @phpstan-import-type Payer from ReceiptWritePersistence
 */
final readonly class ReceiptWriteStore implements ReceiptWritePersistence
{
    public function __construct(private Connection $connection) {}

    public function settings(): ?array
    {
        /** @var Settings|false $row */
        $row = $this->connection->fetchAssociative(
            'SELECT school_name, timezone, default_locale, receipt_prefix, issuer_name, issuer_address, issuer_tax_id, '
            . 'issuer_email, issuer_phone, tax_mode, tax_note, receipt_footer, currency FROM settings',
        );

        return $row === false ? null : $row;
    }

    public function payer(string $id): array
    {
        /** @var Payer|false $row */
        $row = $this->connection->fetchAssociative(
            'SELECT p.given_name, p.family_name, a.email, a.locale FROM persons p '
            . 'LEFT JOIN accounts a ON a.person_id = p.id WHERE p.id = ?',
            [$this->key($id)],
        );

        return $row === false ? throw new RuntimeException('The locked receipt payer disappeared') : $row;
    }

    public function nextSequence(int $year): int
    {
        if ($this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform) {
            $sequence = $this->connection->fetchOne(
                'INSERT INTO receipt_counters (year, last) VALUES (?, 1) '
                . 'ON CONFLICT (year) DO UPDATE SET last = receipt_counters.last + 1 RETURNING last',
                [$year],
            );
        } else {
            // The upsert keeps the year's row locked while this transaction reads and uses the number.
            $this->connection->executeStatement(
                'INSERT INTO receipt_counters (year, last) VALUES (?, 1) ON DUPLICATE KEY UPDATE last = last + 1',
                [$year],
            );
            $sequence = $this->connection->fetchOne('SELECT last FROM receipt_counters WHERE year = ?', [$year]);
        }
        if (!is_numeric($sequence)) {
            throw new RuntimeException('The receipt counter returned no sequence');
        }

        return (int) $sequence;
    }

    public function record(
        string $id,
        int $sequence,
        DateTimeImmutable $issuedAt,
        string $actorId,
        ?string $payerId,
        #[SensitiveParameter]
        ?string $recipientEmail,
        #[SensitiveParameter]
        array $receipt,
        #[SensitiveParameter]
        array $lines,
    ): void {
        $key = $this->key($id);
        $this->connection->insert('receipts', array_diff_key($receipt, ['original_number' => true, 'original_date' => true]) + [
            'id' => $key,
            'year' => (int) substr($receipt['issue_date'], 0, 4),
            'sequence' => $sequence,
            'issued_at' => Database::sqlInstant($this->connection, $issuedAt),
            'created_by' => $this->key($actorId),
            'payer_person_id' => $payerId === null ? null : $this->key($payerId),
            'recipient_email' => $recipientEmail,
            'net' => array_sum(array_column($lines, 'net')),
            'vat' => array_sum(array_column($lines, 'vat')),
        ]);
        foreach ($lines as $line) {
            $this->connection->insert('receipt_lines', ['receipt_id' => $key] + $line);
        }
    }

    public function logo(): string
    {
        return (new PublicSettingsStore($this->connection))->image(BrandingImage::Logo)['data'] ?? '';
    }

    public function putPdf(string $id, #[SensitiveParameter] string $pdf): void
    {
        (new AttachmentStore($this->connection))->put($id, $pdf);
    }

    public function enqueue(string $id, #[SensitiveParameter] string $email, #[SensitiveParameter] Rendered $mail, DateTimeImmutable $now): void
    {
        (new OutboxStore($this->connection))->enqueue($email, 'receipt', $mail, $now, receiptId: $id);
    }

    private function key(string $id): string
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform ? $id : Uuid::fromString($id)->toBinary();
    }
}
