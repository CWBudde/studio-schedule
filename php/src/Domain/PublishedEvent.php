<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

use DateTimeImmutable;

/** @phpstan-import-type NameRef from PublishedLesson */
final readonly class PublishedEvent
{
    /** @param NameRef $courseType */
    public function __construct(
        public string $id,
        public string $slug,
        public string $title,
        public ?string $description,
        public array $courseType,
        public int $capacity,
        public int $price,
        public int $vatRate,
        public ?DateTimeImmutable $signupOpensAt,
        public ?DateTimeImmutable $signupClosesAt,
        public ?DateTimeImmutable $cancelUntil,
        public bool $hasImage,
    ) {}
}
