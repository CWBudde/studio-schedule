<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Domain\BrandingImage;
use StudioSchedule\Domain\Event;
use StudioSchedule\Domain\PublishedLesson;

/** @phpstan-import-type RegistrationCounts from EventPersistence */
final readonly class EventService
{
    /** @param Closure():EventWaitlist $waitlist */
    public function __construct(
        private EventPersistence $store,
        private ClockInterface $clock,
        private OneOffLesson $lessons,
        private Closure $waitlist,
    ) {}

    /** @return array<string,mixed> */
    public function createDraft(EventData $data, SessionPrincipal $principal): array
    {
        return $this->store->write(function () use ($data, $principal): array {
            $slug = $data->slug === '' ? $this->availableSlug($data->derivedSlug()) : $data->slug;
            $data->validate();
            $event = $this->store->createDraft($data, $slug, $principal->impersonatorId ?? $principal->accountId, $this->clock->now());

            return self::officeDocument($event, [], null);
        });
    }

    /** @return array<string,mixed> */
    public function update(string $id, EventData $data): array
    {
        try {
            return $this->store->write(function () use ($id, $data): array {
                $event = $this->locked($id);
                $now = $this->clock->now();
                $sessions = $this->store->sessions([$id]);
                if ($sessions !== [] && $event->courseType['id'] !== $data->courseTypeId) {
                    throw CatalogFailure::field('courseTypeId', 'the course type of an event with sessions cannot be changed');
                }
                $taken = $this->store->occupiedPlaces([$id], $now)[$id] ?? 0;
                if ($data->capacity < $event->capacity && $data->capacity < $taken) {
                    throw CatalogFailure::field('capacity', "$taken places are taken, more than the capacity");
                }
                $data->validate();
                if ($now < $event->createdAt) {
                    throw new CatalogFailure(422, 'an event cannot be updated before it was created');
                }
                $updated = $this->store->updateEvent($id, $data, $data->slug === '' ? $event->slug : $data->slug, $now);
                $this->store->requireSchool();
                $capacityChanged = $updated->capacity !== $event->capacity;
                foreach ($sessions as $session) {
                    // Serialize with concurrent lesson edits when the event's capacity changes.
                    if ($session->startsAt > $now && ($capacityChanged || $session->capacity !== $updated->capacity)) {
                        $this->store->resizeSession($id, $session->id, $updated->capacity, $now);
                    }
                }
                if ($updated->capacity > $event->capacity) {
                    ($this->waitlist)()->offerLocked($updated, $now);
                }

                return $this->eventDetail($updated, $this->store->sessions([$id]), $this->clock->now());
            });
        } catch (SettingsNotFound) {
            throw new CatalogFailure(404, 'resource not found');
        }
    }

    private function availableSlug(string $base): string
    {
        if ($base === '') {
            throw CatalogFailure::field('slug', 'the title gives no slug: choose one of lowercase letters, digits and hyphens');
        }
        for ($number = 1; $number <= 50; ++$number) {
            $slug = $number === 1 ? $base : EventData::numberedSlug($base, $number);
            if (!$this->store->slugInUse($slug)) {
                return $slug;
            }
        }

        throw new CatalogFailure(409, 'a record with these values exists already', reason: 'duplicate');
    }

    /** @return array<string,mixed> */
    public function addSession(string $id, EventSessionData $data): array
    {
        return $this->store->write(function () use ($id, $data): array {
            $rules = $this->lessons->lockRules();
            $event = $this->locked($id);
            if ($event->status === 'canceled') {
                throw new CatalogFailure(409, 'the event takes no registrations', reason: 'closed');
            }

            return $this->lessons->createEventSession(new OneOffLessonData(
                date: $data->date,
                startTime: $data->startTime,
                durationMinutes: $data->durationMinutes,
                roomId: $data->roomId,
                courseTypeId: $event->courseType['id'],
                instructorId: $data->instructorId,
                accompanistId: $data->accompanistId,
                capacity: $event->capacity,
                visibility: 'public',
                kind: 'workshop',
                renter: '',
                confirmRoomOverlap: $data->confirmRoomOverlap,
                confirmPast: $data->confirmPast,
                eventId: $event->id,
            ), $rules);
        });
    }

    /** @return array<string,mixed> */
    public function publish(string $id, bool $publish): array
    {
        return $this->store->write(function () use ($id, $publish): array {
            $event = $this->locked($id);
            $sessions = $this->store->sessions([$event->id]);
            if ($publish) {
                if ($event->status !== 'draft') {
                    throw new CatalogFailure(409, 'the event is not a draft', reason: 'not-draft');
                }
                if (!in_array('scheduled', array_column($sessions, 'status'), true)) {
                    throw new CatalogFailure(409, 'the event has no scheduled session', reason: 'no-sessions');
                }
            } else {
                if ($event->status !== 'published') {
                    throw new CatalogFailure(409, 'the event is not published', reason: 'not-published');
                }
                if ($this->store->hasRegistrations($event->id, onlyOpen: true)) {
                    throw new CatalogFailure(409, 'the record is still in use', reason: 'in-use');
                }
            }

            $now = $this->clock->now();
            $updated = $this->store->setStatus($event->id, $publish ? 'published' : 'draft', $now);

            return $this->eventDetail($updated, $sessions, $now);
        });
    }

    public function delete(string $id): void
    {
        $this->store->write(function () use ($id): void {
            $event = $this->locked($id);
            if ($this->store->hasRegistrations($event->id, onlyOpen: false)) {
                throw new CatalogFailure(409, 'the record is still in use', reason: 'in-use');
            }
            $sessions = $this->store->sessions([$event->id]);
            $now = $this->clock->now();
            foreach ($sessions as $session) {
                if ($session->startsAt <= $now) {
                    throw new CatalogFailure(409, 'the record is still in use', reason: 'in-use');
                }
            }

            $this->store->deleteEvent($event->id, $sessions);
        });
    }

    public function uploadImage(string $id, string $type, #[SensitiveParameter] string $encoded): void
    {
        try {
            $data = AdminSettings::backgroundData($type, $encoded);
        } catch (AdminSettingsFieldError $error) {
            throw CatalogFailure::field($error->field, $error->getMessage());
        }
        $this->store->write(function () use ($id, $type, $data): void {
            $this->locked($id);
            $this->store->setImage($id, $type, $data, BrandingImage::digest($type, $data));
        });
    }

    public function deleteImage(string $id): void
    {
        $this->store->write(function () use ($id): void {
            $this->locked($id);
            $this->store->removeImage($id);
        });
    }

    private function locked(string $id): Event
    {
        return $this->store->lockEvent($id) ?? throw new CatalogFailure(404, 'resource not found');
    }

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
                    => self::publicDocument($event, $sessions[$event->id] ?? [], $occupied[$event->id] ?? 0, $now),
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

            return self::publicDocument($event, $sessions, $occupied[$event->id] ?? 0, $now);
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

            return $this->eventDetail($event, $this->store->sessions([$event->id]), $this->clock->now());
        });
    }

    /**
     * @param list<PublishedLesson> $sessions
     * @return array<string,mixed>
     */
    private function eventDetail(Event $event, array $sessions, DateTimeImmutable $now): array
    {
        $counts = $this->store->registrationCounts([$event->id], $now);
        $image = $this->store->imageInfo($event->id);

        return self::officeDocument($event, $sessions, $counts[$event->id] ?? null) + ($image === null ? [] : ['image' => $image]);
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
     * @return array{?DateTimeImmutable,?DateTimeImmutable,bool}
     */
    public static function signupWindow(Event $event, array $sessions, DateTimeImmutable $now): array
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

        return [$closesAt, $event->cancelUntil ?? $closesAt, $open];
    }

    /**
     * @param list<PublishedLesson> $sessions
     * @return array<string,mixed>
     */
    public static function publicDocument(Event $event, array $sessions, int $occupied, DateTimeImmutable $now): array
    {
        [$closesAt, $cancelUntil, $open] = self::signupWindow($event, $sessions, $now);

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
            'cancelUntil' => self::instant($cancelUntil),
        ], static fn(?string $value): bool => $value !== null && $value !== '');
    }

    /**
     * @param list<PublishedLesson> $sessions
     * @param RegistrationCounts|null $counts
     * @return array<string,mixed>
     */
    public static function officeDocument(Event $event, array $sessions, ?array $counts): array
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
