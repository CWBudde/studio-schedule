<?php

declare(strict_types=1);

namespace StudioSchedule\Push;

use RuntimeException;

final class StatusError extends RuntimeException
{
    public function __construct(public readonly int $statusCode, public readonly string $body, string $reason)
    {
        $status = trim($statusCode . ' ' . $reason);
        parent::__construct('webpush: push service answered ' . $status . ($body === '' ? '' : ': ' . $body));
    }

    public function gone(): bool
    {
        return in_array($this->statusCode, [404, 410], true);
    }

    public function permanent(): bool
    {
        return $this->gone() || in_array($this->statusCode, [400, 401, 403, 413], true);
    }
}
