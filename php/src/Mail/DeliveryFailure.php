<?php

declare(strict_types=1);

namespace StudioSchedule\Mail;

use RuntimeException;

final class DeliveryFailure extends RuntimeException
{
    public function __construct(string $message, public readonly bool $permanent = false)
    {
        parent::__construct($message);
    }
}
