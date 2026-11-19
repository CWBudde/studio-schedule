<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\PublicEvent;
use StudioSchedule\Domain\PublishedLesson;

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

            $ids = array_map(static fn(PublicEvent $event): string => $event->id, $events);
            $sessions = [];
            foreach ($this->store->sessions($ids) as $lesson) {
                assert($lesson->eventId !== null);
                $sessions[$lesson->eventId][] = $lesson;
            }
            $occupied = $this->store->occupiedPlaces($ids, $now);

            return array_map(
                static fn(PublicEvent $event): array
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

    /** @return array{digest:string,contentType:string,data:string}|array{data:null}|null */
    public function image(string $slug): ?array
    {
        $asset = $this->store->image($slug);
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
    private static function document(PublicEvent $event, array $sessions, int $occupied, DateTimeImmutable $now): array
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

    private static function instant(?DateTimeImmutable $instant): ?string
    {
        if ($instant === null) {
            return null;
        }
        $utc = $instant->setTimezone(new DateTimeZone('UTC'));
        $fraction = rtrim($utc->format('u'), '0');

        return $utc->format('Y-m-d\TH:i:s') . ($fraction === '' ? '' : '.' . $fraction) . 'Z';
    }
}
