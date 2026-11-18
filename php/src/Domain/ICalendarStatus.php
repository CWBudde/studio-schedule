<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

enum ICalendarStatus: string
{
    case Confirmed = 'CONFIRMED';
    case Cancelled = 'CANCELLED';
}
