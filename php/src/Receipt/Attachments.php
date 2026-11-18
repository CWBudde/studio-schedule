<?php

declare(strict_types=1);

namespace StudioSchedule\Receipt;

use Closure;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;

/** @phpstan-import-type ReceiptDocument from Renderer */
final readonly class Attachments
{
    /** @param (Closure(ReceiptDocument, string):string)|null $render */
    public function __construct(private AttachmentStore $store, private ?Closure $render = null) {}

    /** @return array{filename: string, pdf: string}|null */
    public function find(string $receiptId): ?array
    {
        $cached = $this->store->find($receiptId);
        if ($cached !== null) {
            return $cached;
        }

        $snapshot = $this->store->snapshot($receiptId);
        if ($snapshot === null) {
            return null;
        }

        // Render with no transaction or receipt lock held.
        $document = Document::fromSnapshot($snapshot['receipt'], $snapshot['lines']);
        $render = $this->render ?? (new Renderer())->render(...);
        $pdf = $render($document, $snapshot['logo']);

        try {
            $this->store->put($receiptId, $pdf);
        } catch (UniqueConstraintViolationException $error) {
            // Go or another PHP request may have stored the immutable original meanwhile.
            return $this->store->find($receiptId) ?? throw $error;
        }

        return ['filename' => $snapshot['receipt']['number'] . '.pdf', 'pdf' => $pdf];
    }
}
