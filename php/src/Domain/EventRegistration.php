<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

use DateTimeImmutable;

/**
 * @phpstan-type Person array{id:string,name:string,anonymized?:true}
 * @phpstan-type Guest array{name:string,email?:string}
 */
final readonly class EventRegistration
{
    /**
     * @param Person|null $person
     * @param Guest|null $guest
     */
    public function __construct(
        public string $id,
        public string $eventId,
        public ?array $person,
        public ?array $guest,
        public string $status,
        public string $payment,
        public int $price,
        public int $vatRate,
        public ?string $receiptId,
        public ?string $cancellationReceiptId,
        public DateTimeImmutable $createdAt,
        public DateTimeImmutable $updatedAt,
        public ?DateTimeImmutable $expiresAt,
        public ?DateTimeImmutable $canceledAt,
    ) {}
}
