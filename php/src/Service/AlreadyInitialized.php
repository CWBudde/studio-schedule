<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use RuntimeException;

final class AlreadyInitialized extends RuntimeException
{
    public function __construct()
    {
        parent::__construct('The school is set up already');
    }
}
