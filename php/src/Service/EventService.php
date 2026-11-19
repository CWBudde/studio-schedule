<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Event;
use StudioSchedule\Domain\PublishedLesson;

/** @phpstan-import-type RegistrationCounts from EventPersistence */
final readonly class EventService
{
    public function __construct(private EventPersistence $store, private ClockInterface $clock) {}

    /** @return list<array<string,mixed>> */
    public function upcoming(): array
    {
        return $this->store->inReadSnapshot(function (): array {
            $now = $this->clock->now();
            $events = $this->store->upcoming($now);
            if ($events === []) {
                return [];
            }

            $ids = array_map(static fn(Event $event): string => $event->id, $events);
            $sessions = $this->sessionsByEvent($ids);
            $occupied = $this->store->occupiedPlaces($ids, $now);

            return array_map(
                static fn(Event $event): array
                    => self::document($event, $sessions[$event->id] ?? [], $occupied[$event->id] ?? 0, $now),
                $events,
            );
        });
    }

    /** @return array<string,mixed>|null */
    public function bySlug(string $slug): ?array
    {
        return $this->store->inReadSnapshot(function () use ($slug): ?array {
            $event = $this->store->bySlug($slug);
            if ($event === null) {
                return null;
            }

            $now = $this->clock->now();
            $sessions = $this->store->sessions([$event->id]);
            $occupied = $this->store->occupiedPlaces([$event->id], $now);

            return self::document($event, $sessions, $occupied[$event->id] ?? 0, $now);
        });
    }

    /** @return list<array<string,mixed>> */
    public function officeList(): array
    {
        return $this->store->inReadSnapshot(function (): array {
            $events = $this->store->officeList();
            if ($events === []) {
                return [];
            }

            $sessions = $this->sessionsByEvent(null);
            $counts = $this->store->registrationCounts(null, $this->clock->now());

            return array_map(
                static fn(Event $event): array => self::officeDocument($event, $sessions[$event->id] ?? [], $counts[$event->id] ?? null),
                $events,
            );
        });
    }

    /** @return array<string,mixed>|null */
    public function officeDetail(string $id): ?array
    {
        return $this->store->inReadSnapshot(function () use ($id): ?array {
            $event = $this->store->officeEvent($id);
            if ($event === null) {
                return null;
            }

            $sessions = $this->store->sessions([$event->id]);
            $counts = $this->store->registrationCounts([$event->id], $this->clock->now());
            $image = $this->store->imageInfo($event->id);

            return self::officeDocument($event, $sessions, $counts[$event->id] ?? null) + ($image === null ? [] : ['image' => $image]);
        });
    }

    /**
     * @param list<string>|null $ids
     * @return array<string,list<PublishedLesson>>
     */
    private function sessionsByEvent(?array $ids): array
    {
        $sessions = [];
        foreach ($this->store->sessions($ids) as $lesson) {
            assert($lesson->eventId !== null);
            $sessions[$lesson->eventId][] = $lesson;
        }

        return $sessions;
    }

    /** @return array{digest:string,contentType:string,data:string}|array{data:null}|null */
    public function image(string $slug): ?array
    {
        return self::imageDocument($this->store->image($slug));
    }

    /** @return array{digest:string,contentType:string,data:string}|array{data:null}|null */
    public function officeImage(string $id): ?array
    {
        return self::imageDocument($this->store->imageById($id));
    }

    /**
     * @param array{contentType:?string,data:?string}|null $asset
     * @return array{digest:string,contentType:string,data:string}|array{data:null}|null
     */
    private static function imageDocument(?array $asset): ?array
    {
        if ($asset === null) {
            return null;
        }
        ['contentType' => $contentType, 'data' => $data] = $asset;
        if ($contentType === null || $data === null) {
            return ['data' => null];
        }

        return ['digest' => hash('sha256', $contentType . "\n" . $data), 'contentType' => $contentType, 'data' => $data];
    }

    /**
     * @param list<PublishedLesson> $sessions
     * @return array<string,mixed>
     */
    private static function document(Event $event, array $sessions, int $occupied, DateTimeImmutable $now): array
    {
        $closesAt = $event->signupClosesAt;
        if ($closesAt === null) {
            foreach ($sessions as $session) {
                if ($session->status === 'scheduled') {
                    $closesAt = $session->startsAt;
                    break;
                }
            }
        }
        $open = $event->status === 'published' && $closesAt !== null && $now < $closesAt
            && ($event->signupOpensAt === null || $now >= $event->signupOpensAt);

        return [
            'slug' => $event->slug,
            'title' => $event->title,
            'courseType' => $event->courseType,
            'capacity' => $event->capacity,
            'freePlaces' => max(0, $event->capacity - $occupied),
            'price' => $event->price,
            'vatRate' => $event->vatRate,
            'status' => $event->status,
            'signupOpen' => $open,
            'hasImage' => $event->hasImage,
            'sessions' => array_map(ScheduleService::lessonSummary(...), $sessions),
        ] + array_filter([
            'description' => $event->description,
            'signupOpensAt' => self::instant($event->signupOpensAt),
            'signupClosesAt' => self::instant($closesAt),
            'cancelUntil' => self::instant($event->cancelUntil ?? $closesAt),
        ], static fn(?string $value): bool => $value !== null && $value !== '');
    }

    /**
     * @param list<PublishedLesson> $sessions
     * @param RegistrationCounts|null $counts
     * @return array<string,mixed>
     */
    private static function officeDocument(Event $event, array $sessions, ?array $counts): array
    {
        return [
            'id' => $event->id,
            'slug' => $event->slug,
            'title' => $event->title,
            'courseTypeId' => $event->courseType['id'],
            'capacity' => $event->capacity,
            'price' => $event->price,
            'vatRate' => $event->vatRate,
            'status' => $event->status,
            'createdAt' => ScheduleService::instant($event->createdAt),
            'updatedAt' => ScheduleService::instant($event->updatedAt),
            'sessions' => array_map(ScheduleService::lessonSummary(...), $sessions),
        ] + ($counts ?? ['registered' => 0, 'invited' => 0, 'waiting' => 0]) + array_filter([
            'description' => $event->description,
            'signupOpensAt' => self::instant($event->signupOpensAt),
            'signupClosesAt' => self::instant($event->signupClosesAt),
            'cancelUntil' => self::instant($event->cancelUntil),
        ], static fn(?string $value): bool => $value !== null && $value !== '');
    }

    private static function instant(?DateTimeImmutable $instant): ?string
    {
        return $instant === null ? null : ScheduleService::instant($instant);
    }
}
