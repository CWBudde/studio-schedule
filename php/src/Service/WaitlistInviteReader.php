<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Domain\ScheduleRules;

interface WaitlistInviteReader
{
    /**
     * @return array{givenName:string,status:string,expiresAt:?DateTimeImmutable,position:int,lesson:PublishedLesson,rules:ScheduleRules}|null
     */
    public function lookup(string $tokenHash): ?array;
}
