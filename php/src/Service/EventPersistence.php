<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use StudioSchedule\Domain\PublishedEvent;
use StudioSchedule\Domain\PublishedLesson;

interface EventPersistence
{
    /**
     * @template T
     * @param Closure():T $read
     * @return T
     */
    public function inReadSnapshot(Closure $read): mixed;

    /** @return list<PublishedEvent> */
    public function upcoming(DateTimeImmutable $now): array;

    /**
     * @param list<string> $eventIds
     * @return list<PublishedLesson>
     */
    public function sessions(array $eventIds): array;

    /**
     * @param list<string> $eventIds
     * @return array<string,int>
     */
    public function occupiedPlaces(array $eventIds, DateTimeImmutable $now): array;
}
