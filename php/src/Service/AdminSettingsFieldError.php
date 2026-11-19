<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use InvalidArgumentException;

final class AdminSettingsFieldError extends InvalidArgumentException
{
    public function __construct(public readonly string $field, string $message)
    {
        parent::__construct($message);
    }
}
