<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

enum CalendarFeedProblem
{
    case NotFound;
    case Forbidden;
    case UnknownLocation;
}
