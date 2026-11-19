<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use RuntimeException;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Domain\ScheduleRules;
use StudioSchedule\Mail\Renderer;

/**
 * @phpstan-import-type Booking from BookingPersistence
 * @phpstan-import-type Settings from EventWaitlistPersistence
 * @phpstan-type Cancellation array{booking:Booking,lesson:PublishedLesson,late:bool}
 */
final readonly class BookingCancellation
{
    /** @param (Closure():void)|null $wakeMail */
    public function __construct(
        private BookingPersistence $store,
        private SchedulePersistence $schedule,
        private EventWaitlistPersistence $messages,
        private LessonWaitlistOffers $offers,
        private Renderer $mail,
        private ClockInterface $clock,
        private ?Closure $wakeMail = null,
    ) {}

    /** @return array{id:string,lessonId:string,personId:string,status:string,source:string,bookedAt:string,canceledAt:string,late:bool} */
    public function cancel(SessionPrincipal $principal, string $id): array
    {
        $result = $this->store->write(function () use ($principal, $id): array {
            $rules = $this->schedule->rules();
            $onBooking = $this->store->redemptionOnBooking();
            [$observed, $person] = $this->owned($principal, $id);
            $this->lockLesson($observed['lessonId']);

            [$booking, , $name] = $this->owned($principal, $id, $observed);
            $lesson = $this->lesson($booking['lessonId']);
            $now = $this->clock->now();
            self::requireActive($lesson, $booking, $now);

            $entry = self::cancellation($booking, $lesson, $rules, $now);
            $settings = $this->messages->settings();
            $this->apply($principal, $person, $entry, $rules, $onBooking, true, $now, $settings);
            $this->notify($principal, $name, [$entry], $now, $settings);

            return [
                'id' => $id, 'lessonId' => $lesson->id, 'personId' => $person, 'status' => 'canceled',
                'source' => $booking['source'], 'bookedAt' => $booking['bookedAt'],
                'canceledAt' => Instant::format($now), 'late' => $entry['late'],
            ];
        });
        $this->wakeMail?->__invoke();

        return $result;
    }

    /** @return array<string,mixed> */
    public function following(SessionPrincipal $principal, string $id, ?Date $until, bool $apply): array
    {
        $work = function () use ($principal, $id, $until, $apply): array {
            $rules = $this->schedule->rules();
            $onBooking = $apply && $this->store->redemptionOnBooking();
            [$observed, $person, $name] = $this->owned($principal, $id);
            $lesson = $this->lesson($observed['lessonId']);
            $now = $this->clock->now();
            $class = self::requireClass($lesson, $observed, $now);
            if ($apply) {
                $this->store->lockClass($class);
            }

            $slot = $this->store->classSlot($lesson->id) ?? throw self::notFound();
            $end = $until ?? $slot['termEnd'];
            if ($end->compare($slot['slot']) < 0) {
                throw new CatalogFailure(422, "the date must not be before the lesson's date", $apply ? 'body.until' : 'query.until');
            }
            $ids = $this->store->followingLessons($class, $slot['slot'], $end, $now);
            if (!in_array($lesson->id, $ids, true)) {
                $ids[] = $lesson->id;
                sort($ids, SORT_STRING);
            }
            if ($apply) {
                // Class writers serialize generation and membership changes. Single lesson
                // writes still overlap, so lock in a stable order and re-read under the locks.
                foreach ($ids as $lessonId) {
                    $this->lockLesson($lessonId);
                }
                // A booking may have been made or a lesson may have started while waiting.
                $now = $this->clock->now();
                [$booking, , $name] = $this->owned($principal, $id, $observed);
                self::requireClass($this->lesson($lesson->id), $booking, $now);
            }

            $chronological = $this->collect($person, $ids, $rules, $now);
            $ordered = self::requestedFirst($chronological, $id);
            $membership = $this->store->membershipPause($class, $person);
            $pause = $membership === null ? null : $end;
            $previous = $membership['pausedUntil'] ?? null;
            if ($previous !== null && $previous->compare($end) > 0) {
                $pause = $previous;
            }
            if ($apply) {
                $settings = $this->messages->settings();
                foreach ($ordered as $entry) {
                    $this->apply($principal, $person, $entry, $rules, $onBooking, $pause === null, $now, $settings);
                }
                if ($pause !== null) {
                    $this->store->pauseMembership($class, $person, $pause);
                }
                $this->notify($principal, $name, $chronological, $now, $settings);
            }

            return [
                'bookings' => array_map(static fn(array $entry): array => [
                    'bookingId' => $entry['booking']['id'], 'lessonId' => $entry['lesson']->id,
                    'startsAt' => Instant::format($entry['lesson']->startsAt), 'endsAt' => Instant::format($entry['lesson']->endsAt),
                    'late' => $entry['late'],
                ], $ordered),
                'until' => (string) $end,
            ] + ($pause === null ? [] : ['pausedUntil' => (string) $pause]);
        };

        if (!$apply) {
            return $this->schedule->inReadSnapshot($work);
        }
        $result = $this->store->write($work);
        $this->wakeMail?->__invoke();

        return $result;
    }

    /**
     * @param Booking|null $observed
     * @return array{Booking,string,string}
     */
    private function owned(SessionPrincipal $principal, string $id, ?array $observed = null): array
    {
        $booking = $this->store->booking($id);
        $person = $booking['personId'] ?? null;
        if ($booking === null || $person === null || ($observed !== null
            && ($observed['personId'] !== $person || $observed['lessonId'] !== $booking['lessonId']))) {
            throw self::notFound();
        }
        $name = $this->schedule->family($principal->personId)[$person] ?? throw self::notFound();

        return [$booking, $person, $name];
    }

    private function lockLesson(string $id): void
    {
        if (!$this->store->lock($id)) {
            throw self::notFound();
        }
    }

    private function lesson(string $id): PublishedLesson
    {
        return $this->schedule->lesson($id) ?? throw self::notFound();
    }

    /** @param Booking $booking */
    private static function requireActive(PublishedLesson $lesson, array $booking, DateTimeImmutable $now): void
    {
        if ($booking['status'] === 'canceled') {
            throw new CatalogFailure(409, 'the booking is canceled already', reason: 'already-canceled');
        }
        if ($now >= $lesson->startsAt) {
            throw new CatalogFailure(422, 'the lesson has started', 'path.bookingId');
        }
    }

    /** @param Booking $booking */
    private static function requireClass(PublishedLesson $lesson, array $booking, DateTimeImmutable $now): string
    {
        if ($lesson->classId === null) {
            throw new CatalogFailure(422, 'the booking is not for a lesson of a class', 'path.bookingId');
        }
        self::requireActive($lesson, $booking, $now);

        return $lesson->classId;
    }

    /**
     * @param Booking $booking
     * @return Cancellation
     */
    private static function cancellation(array $booking, PublishedLesson $lesson, ScheduleRules $rules, DateTimeImmutable $now): array
    {
        $late = $lesson->status !== 'canceled' && $now >= $lesson->startsAt->modify('-' . $rules->cancellationCutoffMicros . ' microseconds');

        return ['booking' => $booking, 'lesson' => $lesson, 'late' => $late];
    }

    /**
     * @param list<string> $ids
     * @return list<Cancellation>
     */
    private function collect(string $person, array $ids, ScheduleRules $rules, DateTimeImmutable $now): array
    {
        $lessons = [];
        foreach ($this->schedule->lessonsById($ids) as $lesson) {
            if ($now < $lesson->startsAt) {
                $lessons[$lesson->id] = $lesson;
            }
        }
        $entries = [];
        foreach ($this->store->activeFor($person, $ids) as $booking) {
            $lesson = $lessons[$booking['lessonId']] ?? null;
            if ($lesson !== null) {
                $entries[] = self::cancellation($booking, $lesson, $rules, $now);
            }
        }
        usort($entries, static fn(array $a, array $b): int => [$a['lesson']->startsAt, $a['lesson']->id] <=> [$b['lesson']->startsAt, $b['lesson']->id]);

        return $entries;
    }

    /**
     * @param list<Cancellation> $entries
     * @return non-empty-list<Cancellation>
     */
    private static function requestedFirst(array $entries, string $requested): array
    {
        foreach ($entries as $i => $entry) {
            if ($entry['booking']['id'] === $requested) {
                unset($entries[$i]);

                return [$entry, ...$entries];
            }
        }

        throw self::notFound();
    }

    /**
     * @param Cancellation $entry
     * @param Settings $settings
     */
    private function apply(
        SessionPrincipal $principal,
        string $person,
        array $entry,
        ScheduleRules $rules,
        bool $onBooking,
        bool $released,
        DateTimeImmutable $now,
        array $settings,
    ): void {
        ['booking' => $booking, 'lesson' => $lesson, 'late' => $late] = $entry;
        if ($now < new DateTimeImmutable($booking['bookedAt'])) {
            throw new CatalogFailure(422, 'validation failed: booking cannot be canceled before it was made');
        }

        $this->store->cancelLocked($booking['id'], $now, $late, $released);
        if ($lesson->status !== 'canceled' && $booking['source'] !== 'member' && !$booking['trial']) {
            $day = (string) Date::fromInstant($lesson->startsAt, $rules->timezone);
            $this->settle($booking, $person, $onBooking, $late, $day, $now);
        }
        $this->store->record($booking['id'], $principal->impersonatorId ?? $principal->accountId, 'canceled', $now, $late);

        // A new invitation uses the current booking cutoff under the lesson lock.
        $this->offers->offerLocked($lesson, $this->schedule->rules(), $settings, $now);
    }

    /** @param Booking $booking */
    private function settle(array $booking, string $person, bool $onBooking, bool $late, string $day, DateTimeImmutable $now): void
    {
        $wasDue = $onBooking || in_array($booking['status'], ['attended', 'no_show'], true);
        if ($late && !$wasDue) {
            $this->store->settle([['id' => $booking['id'], 'personId' => $person]], [], $day, $now);
        }
        if (!$late && $wasDue) {
            $this->store->settle([], [$booking['id']], $day, $now);
        }
    }

    /**
     * @param list<Cancellation> $entries
     * @param Settings $settings
     */
    private function notify(SessionPrincipal $principal, string $participant, array $entries, DateTimeImmutable $now, array $settings): void
    {
        $recipient = $this->store->recipient($principal->accountId);
        if ($recipient === null) {
            throw self::notFound();
        }
        if (!SetupInput::validEmail($recipient['email'])) {
            throw new RuntimeException('The booking cancellation recipient has an invalid email address');
        }
        $locale = $this->mail->resolveLocale($recipient['locale'] ?: $settings['locale']);
        $rendered = $this->mail->render($locale, 'booking-canceled', [
            'SchoolName' => $settings['name'], 'GivenName' => $recipient['givenName'], 'Participant' => $participant,
            'Lessons' => array_map(static function (array $entry) use ($settings): array {
                $lesson = $entry['lesson'];

                return [
                    'CourseType' => $lesson->courseType['name'] ?? '',
                    'StartsAt' => $lesson->startsAt->setTimezone($settings['timezone']),
                    'EndsAt' => $lesson->endsAt->setTimezone($settings['timezone']),
                    'Room' => $lesson->room['name'], 'Location' => $lesson->location['name'], 'Late' => $entry['late'],
                ];
            }, $entries),
        ]);
        $this->messages->enqueueMail($recipient['email'], 'booking-canceled', $rendered, $now);
    }

    private static function notFound(): CatalogFailure
    {
        return new CatalogFailure(404, 'resource not found');
    }
}
