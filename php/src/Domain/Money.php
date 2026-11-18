<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

use JsonSerializable;

/** An integer amount in the minor units of the school's currency. */
final readonly class Money implements JsonSerializable
{
    public function __construct(public int $minorUnits) {}

    public function jsonSerialize(): int
    {
        return $this->minorUnits;
    }
}
