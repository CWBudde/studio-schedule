<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use StudioSchedule\Domain\Event;
use StudioSchedule\Domain\PublishedLesson;

/** @phpstan-type RegistrationCounts array{registered:int,invited:int,waiting:int} */
interface EventPersistence
{
    /**
     * @template T
     * @param Closure():T $read
     * @return T
     */
    public function inReadSnapshot(Closure $read): mixed;

    /** @return list<Event> */
    public function upcoming(DateTimeImmutable $now): array;

    public function bySlug(string $slug): ?Event;

    /** @return array{contentType:?string,data:?string}|null */
    public function image(string $slug): ?array;

    /** @return list<Event> */
    public function officeList(): array;

    public function officeEvent(string $id): ?Event;

    /** @return array{contentType:string,digest:string}|null */
    public function imageInfo(string $id): ?array;

    /** @return array{contentType:?string,data:?string}|null */
    public function imageById(string $id): ?array;

    /**
     * @param list<string>|null $eventIds Null selects all events.
     * @return array<string,RegistrationCounts>
     */
    public function registrationCounts(?array $eventIds, DateTimeImmutable $now): array;

    /**
     * @param list<string>|null $eventIds Null selects all events.
     * @return list<PublishedLesson>
     */
    public function sessions(?array $eventIds): array;

    /**
     * @param list<string> $eventIds
     * @return array<string,int>
     */
    public function occupiedPlaces(array $eventIds, DateTimeImmutable $now): array;
}
