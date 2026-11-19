<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use RuntimeException;
use StudioSchedule\Domain\PublishedLesson;

final class LessonRoomTaken extends RuntimeException
{
    public function __construct(public readonly PublishedLesson $lesson)
    {
        parent::__construct('another lesson uses the room at that time');
    }
}
