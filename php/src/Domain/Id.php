<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

final class Id
{
    public static function valid(string $id): bool
    {
        return $id !== '00000000-0000-0000-0000-000000000000'
            && preg_match('/\A[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}\z/i', $id) === 1;
    }
}
