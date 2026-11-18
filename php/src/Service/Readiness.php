<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

interface Readiness
{
    public function ready(): bool;
}
