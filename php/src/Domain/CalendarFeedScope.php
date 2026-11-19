<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

enum CalendarFeedScope: string
{
    case Bookings = 'bookings';
    case Teaching = 'teaching';
    case Schedule = 'schedule';
}
