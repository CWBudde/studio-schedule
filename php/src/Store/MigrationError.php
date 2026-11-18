<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use RuntimeException;

/** A migration error whose message contains no connection or database details. */
final class MigrationError extends RuntimeException
{
    public function __construct(string $message, public readonly bool $needsBackup = false)
    {
        parent::__construct($message);
    }
}
