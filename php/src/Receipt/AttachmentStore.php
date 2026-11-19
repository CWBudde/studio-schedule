<?php

declare(strict_types=1);

namespace StudioSchedule\Receipt;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\BrandingImage;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\PublicSettingsStore;

/**
 * @phpstan-type ReceiptRow array{
 *     number:string, kind:'sale'|'cancellation', issue_date:string, locale:string, issuer_name:string,
 *     issuer_address:string, issuer_tax_id:string, issuer_email:?string, issuer_phone:?string, tax_mode:string,
 *     tax_note:?string, footer:?string, recipient_name:string, recipient_address:?string,
 *     payment_method:'cash'|'transfer'|'card', currency:string, gross:int|numeric-string,
 *     reason:?string, original_number:?string, original_date:?string
 * }
 * @phpstan-type ReceiptLine array{
 *     position:int|numeric-string, description:string, quantity:int|numeric-string, unit_price:int|numeric-string,
 *     vat_rate_bp:int|numeric-string, net:int|numeric-string, vat:int|numeric-string, gross:int|numeric-string
 * }
 */
final readonly class AttachmentStore
{
    public function __construct(private Connection $db) {}

    /** @return array{filename: string, pdf: string}|null */
    public function find(string $receiptId): ?array
    {
        $key = $this->key($receiptId);
        if ($key === null) {
            return null;
        }

        $id = $this->idParameter();
        /** @var array{string, string|resource}|false $row */
        $row = $this->db->fetchNumeric(
            'SELECT r.number, p.pdf FROM receipt_pdfs p JOIN receipts r ON r.id = p.receipt_id '
            . "WHERE p.receipt_id = $id",
            [$key],
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

    public function canDownload(string $receiptId, string $personId): bool
    {
        $receipt = $this->key($receiptId);
        $person = $this->key($personId);
        if ($receipt === null || $person === null) {
            return false;
        }

        $id = $this->idParameter();

        return $this->db->fetchOne(
            "SELECT 1 FROM receipts r WHERE r.id = $id AND (r.payer_person_id = $id "
            . "OR EXISTS (SELECT 1 FROM persons p WHERE p.id = r.payer_person_id AND p.merged_into = $id) "
            . 'OR EXISTS (SELECT 1 FROM event_registrations e WHERE e.receipt_id = COALESCE(r.cancels_receipt_id, r.id) '
            . "AND (e.person_id = $id OR EXISTS (SELECT 1 FROM person_guardians g "
            . "WHERE g.dependent_id = e.person_id AND g.guardian_id = $id))))",
            [$receipt, $person, $person, $person, $person],
        ) !== false;
    }

    /** @return array{receipt:ReceiptRow, lines:list<ReceiptLine>, logo:string}|null */
    public function snapshot(string $receiptId): ?array
    {
        $key = $this->key($receiptId);
        if ($key === null) {
            return null;
        }

        return Database::inReadSnapshot($this->db, function () use ($key): ?array {
            $id = $this->idParameter();
            /** @var ReceiptRow|false $receipt */
            $receipt = $this->db->fetchAssociative(
                'SELECT r.number, r.kind, r.issue_date, r.locale, r.issuer_name, r.issuer_address, r.issuer_tax_id, '
                . 'r.issuer_email, r.issuer_phone, r.tax_mode, r.tax_note, r.footer, r.recipient_name, r.recipient_address, '
                . 'r.payment_method, r.currency, r.gross, r.reason, original.number AS original_number, original.issue_date AS original_date '
                . "FROM receipts r LEFT JOIN receipts original ON original.id = r.cancels_receipt_id WHERE r.id = $id",
                [$key],
            );
            if ($receipt === false) {
                return null;
            }

            /** @var list<ReceiptLine> $lines */
            $lines = $this->db->fetchAllAssociative(
                'SELECT position, description, quantity, unit_price, vat_rate_bp, net, vat, gross '
                . "FROM receipt_lines WHERE receipt_id = $id ORDER BY position",
                [$key],
            );
            $logo = (new PublicSettingsStore($this->db))->image(BrandingImage::Logo);
            if ($logo === null) {
                throw new RuntimeException('School settings are missing for receipt rendering');
            }

            return ['receipt' => $receipt, 'lines' => $lines, 'logo' => $logo['data'] ?? ''];
        });
    }

    public function put(string $receiptId, #[SensitiveParameter] string $pdf): void
    {
        $key = $this->key($receiptId);
        if ($key === null || $pdf === '') {
            throw new RuntimeException('Cannot store a receipt PDF without a valid receipt and bytes');
        }

        $id = $this->idParameter();
        $this->db->executeStatement(
            "INSERT INTO receipt_pdfs (receipt_id, pdf) VALUES ($id, ?)",
            [$key, $pdf],
            [ParameterType::STRING, ParameterType::BINARY],
        );
    }

    private function key(string $receiptId): ?string
    {
        if (preg_match('/\A[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}\z/i', $receiptId) !== 1) {
            return null;
        }

        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform ? $receiptId : str_replace('-', '', $receiptId);
    }

    private function idParameter(): string
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform ? '?' : 'UNHEX(?)';
    }
}
