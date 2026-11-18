<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

final readonly class ICalendarDocument
{
    /** The fingerprint is the SHA-256 of the encoded body with DTSTAMP lines excluded. */
    public function __construct(public string $body, public string $fingerprint) {}
}
