<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use RuntimeException;

final class LessonNoticeLimit extends RuntimeException
{
    public function __construct(public readonly int $retryAfter)
    {
        parent::__construct("too many notices; try again in $retryAfter seconds");
    }
}
