<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use RuntimeException;

final class CatalogFailure extends RuntimeException
{
    public function __construct(
        public readonly int $status,
        string $message,
        public readonly string $location = '',
        public readonly string $reason = '',
        public readonly string $why = '',
    ) {
        parent::__construct($message);
    }

    public static function field(string $field, string $message): self
    {
        return new self(422, $message, 'body.' . $field);
    }
}
