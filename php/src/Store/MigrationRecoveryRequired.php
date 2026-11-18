<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use RuntimeException;
use Throwable;

final class MigrationRecoveryRequired extends RuntimeException
{
    public function __construct(?Throwable $previous = null)
    {
        parent::__construct(
            'MySQL or MariaDB migration stopped. Replace the database with the verified pre-upgrade backup before retrying. '
            . 'For a first install, start over with an empty database.',
            0,
            $previous,
        );
    }
}
