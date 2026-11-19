<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

use DateTimeImmutable;
use DateTimeZone;

final class Instant
{
    public static function format(DateTimeImmutable $instant): string
    {
        $utc = $instant->setTimezone(new DateTimeZone('UTC'));
        $fraction = rtrim($utc->format('u'), '0');

        return $utc->format('Y-m-d\TH:i:s') . ($fraction === '' ? '' : '.' . $fraction) . 'Z';
    }
}
