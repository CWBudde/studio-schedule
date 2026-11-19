<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use StudioSchedule\Mail\Rendered;

/**
 * @phpstan-import-type ReceiptRow from \StudioSchedule\Receipt\AttachmentStore
 * @phpstan-import-type ReceiptLine from \StudioSchedule\Receipt\AttachmentStore
 * @phpstan-type Settings array{
 *     school_name:string, timezone:string, default_locale:string, currency:string, receipt_prefix:string,
 *     issuer_name:?string, issuer_address:?string, issuer_tax_id:?string, issuer_email:?string, issuer_phone:?string,
 *     tax_mode:?string, tax_note:?string, receipt_footer:?string
 * }
 * @phpstan-type Payer array{given_name:string,family_name:string,email:?string,locale:?string}
 */
interface ReceiptWritePersistence
{
    /** @return Settings|null */
    public function settings(): ?array;

    /** @return Payer */
    public function payer(string $id): array;

    public function nextSequence(int $year): int;

    /**
     * @param ReceiptRow $receipt
     * @param list<ReceiptLine> $lines
     */
    public function record(
        string $id,
        int $sequence,
        DateTimeImmutable $issuedAt,
        string $actorId,
        ?string $payerId,
        ?string $recipientEmail,
        array $receipt,
        array $lines,
    ): void;

    public function logo(): string;

    public function putPdf(string $id, string $pdf): void;

    public function enqueue(string $id, string $email, Rendered $mail, DateTimeImmutable $now): void;
}
