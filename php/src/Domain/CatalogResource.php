<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

enum CatalogResource: string
{
    case Location = 'locations';
    case Room = 'rooms';
    case CourseType = 'course-types';
    case Level = 'levels';
    case Staff = 'staff';

    public function idField(): string
    {
        return match ($this) {
            self::Location => 'locationId',
            self::Room => 'roomId',
            self::CourseType => 'courseTypeId',
            self::Level => 'levelId',
            self::Staff => 'staffId',
        };
    }
}
