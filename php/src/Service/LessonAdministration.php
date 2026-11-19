<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use RuntimeException;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Event;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Mail\Renderer;

/** @phpstan-import-type Settings from EventWaitlistPersistence */
final readonly class LessonAdministration
{
    /** @param (Closure():void)|null $wakeMail */
    public function __construct(
        private EventPersistence $events,
        private LessonWritePersistence $writes,
        private SchedulePersistence $schedule,
        private LessonRecipients $participants,
        private EventWaitlistPersistence $recipients,
        private OneOffLessonPersistence $timetable,
        private LessonWaitlistOffers $offers,
        private Renderer $mail,
        private ClockInterface $clock,
        private ?Closure $wakeMail = null,
    ) {}

    /** @return array<string,mixed> */
    public function update(string $id, OneOffLessonData $data): array
    {
        return $this->writeAndWake(function () use ($id, $data): array {
            $rules = $this->timetable->lockRules();
            $settings = $this->recipients->settings();
            [$old, $event] = $this->lock($id);
            self::requireFuture($old, $this->clock->now());

            $data->validate($old);
            [$start, $end] = $data->instants($rules);
            $this->timetable->lockLessonReferences($data);
            $now = $this->clock->now();
            $this->requireAllowedChange($old, $event, $data, $now);
            if (!$data->confirmPast && $start <= $now) {
                throw new CatalogFailure(409, 'the lesson would start in the past', reason: 'past');
            }
            $moved = $start != $old->startsAt || $data->roomId !== $old->room['id'];
            if (!$data->confirmRoomOverlap && $old->status === 'scheduled' && ($moved || $end != $old->endsAt)) {
                $this->requireRoom($data->roomId, $start, $end, $id);
            }

            $this->writes->updateLocked($id, $data, $start, $end);
            $day = Date::fromInstant($start, $rules->timezone);
            if ($day->compare(Date::fromInstant($old->startsAt, $rules->timezone)) !== 0) {
                $this->writes->recheckPayments($id, (string) $day, $now);
            }
            $lesson = $this->storedLesson($id);
            $queued = $moved && $lesson->status === 'scheduled' && $this->notify($lesson, $settings, 'lesson-changed', $old);
            $offered = $this->offers->offerLocked($lesson, $rules, $settings, $now);

            return [(new ScheduleService($this->schedule, $this->clock))->officeWriteDocument($lesson, $rules), $queued || $offered];
        });
    }

    /** @return array<string,mixed> */
    public function restore(string $id, bool $confirmRoomOverlap): array
    {
        return $this->writeAndWake(function () use ($id, $confirmRoomOverlap): array {
            // Match creation's settings lock before checking room availability.
            $rules = $this->timetable->lockRules();
            $settings = $this->recipients->settings();
            [$lesson, $event] = $this->lock($id);
            $queued = false;
            if ($lesson->status === 'canceled') {
                $now = $this->clock->now();
                self::requireFuture($lesson, $now);
                if ($event?->status === 'canceled') {
                    throw new CatalogFailure(409, 'the event of the session is canceled', reason: 'closed');
                }
                if (!$confirmRoomOverlap) {
                    $this->requireRoom($lesson->room['id'], $lesson->startsAt, $lesson->endsAt, $id);
                }

                $day = Date::fromInstant($lesson->startsAt, $rules->timezone);
                $this->writes->restoreLocked($id, (string) $day, $now, $this->writes->redemptionOnBooking());
                $lesson = $this->storedLesson($id);
                $notified = $this->notify($lesson, $settings, 'lesson-restored');
                $offered = $this->offers->offerLocked($lesson, $rules, $settings, $now);
                $queued = $notified || $offered;
            }

            return [(new ScheduleService($this->schedule, $this->clock))->officeWriteDocument($lesson, $rules), $queued];
        });
    }

    public function delete(string $id): void
    {
        $this->events->write(function () use ($id): void {
            [$lesson] = $this->lock($id);
            if ($lesson->classId !== null) {
                throw new CatalogFailure(409, 'a lesson of a class is canceled, not deleted', reason: 'generated');
            }
            self::requireFuture($lesson, $this->clock->now());
            if ($this->writes->hasBookings($id) || ($lesson->eventId !== null && $this->events->hasRegistrations($lesson->eventId, onlyOpen: true))) {
                throw new CatalogFailure(409, 'the record is still in use', reason: 'in-use');
            }

            $this->writes->delete($id);
        });
    }

    /** @return array<string,mixed> */
    public function cancel(string $id, string $note, SessionPrincipal $principal): array
    {
        return $this->writeAndWake(function () use ($id, $note, $principal): array {
            $settings = $this->recipients->settings();
            $rules = $this->schedule->rules();
            $onBooking = $this->writes->redemptionOnBooking();
            [$lesson] = $this->lock($id);
            $queued = false;
            if ($lesson->status !== 'canceled') {
                $now = $this->clock->now();
                self::requireFuture($lesson, $now);
                if (mb_strlen($note, 'UTF-8') > 500) {
                    throw CatalogFailure::field('note', 'the note is too long');
                }
                $actor = $principal->impersonatorId ?? $principal->accountId;
                $this->writes->cancelLocked($id, $actor, $note, $now, $onBooking);
                $lesson = $this->storedLesson($id);
                $queued = $this->notify($lesson, $settings, 'lesson-canceled');
            }

            return [(new ScheduleService($this->schedule, $this->clock))->officeWriteDocument($lesson, $rules), $queued];
        });
    }

    /**
     * @param Closure():array{array<string,mixed>,bool} $write
     * @return array<string,mixed>
     */
    private function writeAndWake(Closure $write): array
    {
        try {
            [$document, $queued] = $this->events->write($write);
        } catch (SettingsNotFound) {
            throw self::notFound();
        }
        if ($queued) {
            $this->wakeMail?->__invoke();
        }

        return $document;
    }

    /** @return array{PublishedLesson,?Event} */
    private function lock(string $id): array
    {
        $observed = $this->schedule->lesson($id, storedCapacity: true) ?? throw self::notFound();
        // Event membership is permanent. Lock the event before taking any lesson row lock.
        $event = $observed->eventId === null ? null : ($this->events->lockEvent($observed->eventId) ?? throw self::notFound());
        if (!$this->writes->lock($id)) {
            throw self::notFound();
        }

        return [$this->schedule->lesson($id, storedCapacity: true) ?? throw self::notFound(), $event];
    }

    private function storedLesson(string $id): PublishedLesson
    {
        return $this->schedule->lesson($id, storedCapacity: true) ?? throw new RuntimeException('The updated lesson disappeared');
    }

    private function requireAllowedChange(PublishedLesson $old, ?Event $event, OneOffLessonData $data, DateTimeImmutable $now): void
    {
        if ($event !== null) {
            if ($data->capacity !== $event->capacity) {
                throw CatalogFailure::field('capacity', "a session has its event's capacity, {$event->capacity}");
            }
            if ($data->visibility !== 'public') {
                throw CatalogFailure::field('visibility', "a session is public; its event's status decides who sees it");
            }
        }
        if ($data->kind === 'rental' && $old->kind !== 'rental') {
            if ($this->writes->hasBookings($old->id) || ($this->schedule->waitlist([$old->id])[$old->id] ?? []) !== []) {
                throw CatalogFailure::field('kind', 'a lesson with bookings or a waiting list cannot become a room rental');
            }

            return;
        }
        if ($data->capacity < $old->capacity) {
            $active = $this->schedule->activeBookings([$old->id])[$old->id] ?? 0;
            $held = ScheduleService::heldPlaces($this->schedule->waitlist([$old->id])[$old->id] ?? [], $now);
            if ($active + $held > $data->capacity) {
                $reserved = $held === 0 ? '' : " and $held places reserved for the waiting list";
                throw CatalogFailure::field('capacity', "the lesson has $active bookings$reserved, more than the capacity");
            }
        }
    }

    private function requireRoom(string $room, DateTimeImmutable $start, DateTimeImmutable $end, string $id): void
    {
        $overlap = $this->timetable->roomOverlap($room, $start, $end, excluding: $id);
        if ($overlap !== null) {
            throw new LessonRoomTaken($overlap);
        }
    }

    /** @param Settings $settings */
    private function notify(PublishedLesson $lesson, array $settings, string $template, ?PublishedLesson $previous = null): bool
    {
        $now = $this->clock->now();
        if ($lesson->startsAt <= $now) {
            return false;
        }
        $messages = $this->participants->forLesson($lesson);
        $zone = $settings['timezone'];
        $priorPlace = $previous === null ? [] : [
            'PreviousStartsAt' => $previous->startsAt->setTimezone($zone), 'PreviousEndsAt' => $previous->endsAt->setTimezone($zone),
            'PreviousRoom' => $previous->room['name'], 'PreviousLocation' => $previous->location['name'],
        ];
        foreach ($messages as $message) {
            $to = $message['recipient'];
            if (!SetupInput::validEmail($to['email'])) {
                $notice = $template === 'lesson-canceled' ? 'cancellation' : 'update';
                throw new RuntimeException("A lesson $notice recipient has an invalid email address");
            }
            $locale = $to['locale'] === '' ? $settings['locale'] : $to['locale'];
            $mail = $this->mail->render($locale, $template, [
                'SchoolName' => $settings['name'], 'GivenName' => $to['givenName'], 'Participants' => $message['participants'],
                'CourseType' => $lesson->courseType['name'] ?? '', 'StartsAt' => $lesson->startsAt->setTimezone($zone),
                'EndsAt' => $lesson->endsAt->setTimezone($zone), 'Room' => $lesson->room['name'],
                'Location' => $lesson->location['name'], 'Note' => $lesson->cancelNote ?? '',
            ] + $priorPlace);
            $this->recipients->enqueueMail($to['email'], $template, $mail, $now);
        }

        return $messages !== [];
    }

    private static function requireFuture(PublishedLesson $lesson, DateTimeImmutable $now): void
    {
        if ($lesson->startsAt <= $now) {
            throw new CatalogFailure(422, 'the lesson has started', 'path.lessonId');
        }
    }

    private static function notFound(): CatalogFailure
    {
        return new CatalogFailure(404, 'resource not found');
    }
}
