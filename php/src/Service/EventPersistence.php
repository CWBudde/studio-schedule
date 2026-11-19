<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use StudioSchedule\Domain\PublicEvent;
use StudioSchedule\Domain\PublishedLesson;

interface EventPersistence
{
    /**
     * @template T
     * @param Closure():T $read
     * @return T
     */
    public function inReadSnapshot(Closure $read): mixed;

    /** @return list<PublicEvent> */
    public function upcoming(DateTimeImmutable $now): array;

    public function bySlug(string $slug): ?PublicEvent;

    /** @return array{contentType:?string,data:?string}|null */
    public function image(string $slug): ?array;

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
