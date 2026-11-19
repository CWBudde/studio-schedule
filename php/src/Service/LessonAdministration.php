<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use RuntimeException;
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
        private EventRegistrationPersistence $registrations,
        private EventWaitlistPersistence $recipients,
        private Renderer $mail,
        private ClockInterface $clock,
        private ?Closure $wakeMail = null,
    ) {}

    public function delete(string $id): void
    {
        $this->events->write(function () use ($id): void {
            $lesson = $this->lock($id);
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
        try {
            [$document, $queued] = $this->events->write(function () use ($id, $note, $principal): array {
                $settings = $this->recipients->settings();
                $rules = $this->schedule->rules();
                $onBooking = $this->writes->redemptionOnBooking();
                $lesson = $this->lock($id);
                $queued = false;
                if ($lesson->status !== 'canceled') {
                    $now = $this->clock->now();
                    self::requireFuture($lesson, $now);
                    if (mb_strlen($note, 'UTF-8') > 500) {
                        throw CatalogFailure::field('note', 'the note is too long');
                    }
                    $actor = $principal->impersonatorId ?? $principal->accountId;
                    $this->writes->cancelLocked($id, $actor, $note, $now, $onBooking);
                    $lesson = $this->schedule->lesson($id, storedCapacity: true) ?? throw new RuntimeException('The canceled lesson disappeared');
                    $mailAt = $this->clock->now();
                    if ($lesson->startsAt > $mailAt) {
                        $queued = $this->notify($lesson, $settings, $note, $mailAt);
                    }
                }

                return [(new ScheduleService($this->schedule, $this->clock))->officeWriteDocument($lesson, $rules), $queued];
            });
        } catch (SettingsNotFound) {
            throw self::notFound();
        }
        if ($queued) {
            $this->wakeMail?->__invoke();
        }

        return $document;
    }

    private function lock(string $id): PublishedLesson
    {
        $observed = $this->schedule->lesson($id, storedCapacity: true) ?? throw self::notFound();
        // Event membership is permanent. Lock the event before taking any lesson row lock.
        if ($observed->eventId !== null && $this->events->lockEvent($observed->eventId) === null) {
            throw new RuntimeException('The lesson references a missing event');
        }
        if (!$this->writes->lock($id)) {
            throw self::notFound();
        }

        return $this->schedule->lesson($id, storedCapacity: true) ?? throw self::notFound();
    }

    /** @param Settings $settings */
    private function notify(PublishedLesson $lesson, array $settings, string $note, DateTimeImmutable $now): bool
    {
        $parties = $this->writes->bookedRecipients($lesson->id);
        if ($lesson->eventId !== null) {
            foreach ($this->registrations->forEvent($lesson->eventId) as $registration) {
                if ($registration->status === 'registered') {
                    $parties[] = ['personId' => $registration->person['id'] ?? null, 'guest' => $registration->guest];
                }
            }
        }
        $messages = (new ScheduleRecipients($this->recipients))->group($parties);
        foreach ($messages as $message) {
            $to = $message['recipient'];
            if (!SetupInput::validEmail($to['email'])) {
                throw new RuntimeException('A lesson cancellation recipient has an invalid email address');
            }
            $locale = $to['locale'] === '' ? $settings['locale'] : $to['locale'];
            $mail = $this->mail->render($locale, 'lesson-canceled', [
                'SchoolName' => $settings['name'], 'GivenName' => $to['givenName'], 'Participants' => $message['participants'],
                'CourseType' => $lesson->courseType['name'] ?? '', 'StartsAt' => $lesson->startsAt->setTimezone($settings['timezone']),
                'EndsAt' => $lesson->endsAt->setTimezone($settings['timezone']), 'Room' => $lesson->room['name'],
                'Location' => $lesson->location['name'], 'Note' => $note,
            ]);
            $this->recipients->enqueueMail($to['email'], 'lesson-canceled', $mail, $now);
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
