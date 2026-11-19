<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use RuntimeException;

final class AnnouncementProblem extends RuntimeException
{
    public function __construct(
        public readonly int $status,
        string $message,
        public readonly string $fieldName = '',
        public readonly string $reason = '',
    ) {
        parent::__construct($message);
    }

    public static function field(string $field, string $message): self
    {
        return new self(422, $message, $field);
    }
}
