<?php

declare(strict_types=1);

namespace StudioSchedule\Console;

use RuntimeException;

final class CommandError extends RuntimeException
{
    public function __construct(string $message, public readonly int $status = 1)
    {
        parent::__construct($message);
    }
}
