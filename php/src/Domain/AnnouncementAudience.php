<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

final readonly class AnnouncementAudience
{
    public const array ID_FIELDS = [
        'class' => 'classId', 'course-type' => 'courseTypeId', 'lesson' => 'lessonId', 'event' => 'eventId',
    ];

    public function __construct(public string $kind, public ?string $role, public ?string $reference) {}
}
