<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

interface PersonDetailReader
{
    /** @return array<string,mixed>|null */
    public function get(string $id): ?array;
}
