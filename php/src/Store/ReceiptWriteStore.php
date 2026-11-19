<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\BrandingImage;
use StudioSchedule\Mail\Rendered;
use StudioSchedule\Receipt\AttachmentStore;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\ReceiptWritePersistence;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type Settings from ReceiptWritePersistence
 * @phpstan-import-type Payer from ReceiptWritePersistence
 * @phpstan-import-type Original from ReceiptWritePersistence
 * @phpstan-import-type ReceiptLine from AttachmentStore
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
            'SELECT recipient.given_name, recipient.family_name, a.email, a.locale FROM persons p '
            . 'JOIN persons recipient ON recipient.id = COALESCE(p.merged_into, p.id) '
            . 'LEFT JOIN accounts a ON a.person_id = recipient.id WHERE p.id = ?',
            [$this->key($id)],
        );

        return $row === false ? throw new RuntimeException('The locked receipt payer disappeared') : $row;
    }

    public function original(string $id): ?array
    {
        $key = $this->key($id);
        /** @var array{kind:'sale'|'cancellation',number:string,issue_date:string,locale:string,tax_mode:string,tax_note:?string,
         *     currency:string,gross:int|numeric-string,recipient_name:string,recipient_address:?string,recipient_email:?string,
         *     payment_method:'cash'|'transfer'|'card',payer_person_id:?string,has_cancellation:bool|int|string}|false $row
         */
        $row = $this->connection->fetchAssociative(
            'SELECT r.kind, r.number, r.issue_date, r.locale, r.tax_mode, r.tax_note, r.currency, r.gross, '
            . 'r.recipient_name, r.recipient_address, r.recipient_email, r.payment_method, r.payer_person_id, '
            . 'EXISTS (SELECT 1 FROM receipts c WHERE c.cancels_receipt_id = r.id) AS has_cancellation '
            . 'FROM receipts r WHERE r.id = ?',
            [$key],
        );
        if ($row === false) {
            return null;
        }
        /** @var list<ReceiptLine> $lines */
        $lines = $this->connection->fetchAllAssociative(
            'SELECT position, description, quantity, unit_price, vat_rate_bp, net, vat, gross '
            . 'FROM receipt_lines WHERE receipt_id = ? ORDER BY position',
            [$key],
        );
        $payer = $row['payer_person_id'];
        $hasCancellation = in_array($row['has_cancellation'], [true, 1, '1', 't'], true);
        unset($row['payer_person_id'], $row['has_cancellation']);

        return $row + [
            'payerId' => $payer === null ? null : ($this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform ? $payer : Uuid::fromBinary($payer)->toRfc4122()),
            'hasCancellation' => $hasCancellation, 'lines' => $lines,
        ];
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
        ?string $cancelsReceiptId = null,
    ): void {
        $key = $this->key($id);
        try {
            $this->connection->insert('receipts', array_diff_key($receipt, ['original_number' => true, 'original_date' => true]) + [
                'id' => $key,
                'year' => (int) substr($receipt['issue_date'], 0, 4),
                'sequence' => $sequence,
                'cancels_receipt_id' => $cancelsReceiptId === null ? null : $this->key($cancelsReceiptId),
                'issued_at' => Database::sqlInstant($this->connection, $issuedAt),
                'created_by' => $this->key($actorId),
                'payer_person_id' => $payerId === null ? null : $this->key($payerId),
                'recipient_email' => $recipientEmail,
                'net' => array_sum(array_column($lines, 'net')),
                'vat' => array_sum(array_column($lines, 'vat')),
            ]);
        } catch (UniqueConstraintViolationException $error) {
            if ($cancelsReceiptId !== null && str_contains($error->getMessage(), 'receipts_cancels_key')) {
                throw new CatalogFailure(409, 'the receipt is canceled already', reason: 'already-canceled');
            }
            throw $error;
        }
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
