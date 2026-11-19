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
use StudioSchedule\Mail\Renderer;

/**
 * @phpstan-import-type Booking from BookingPersistence
 * @phpstan-import-type Settings from EventWaitlistPersistence
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
            $observed = $this->store->booking($id);
            $person = $observed['personId'] ?? null;
            if ($observed === null || $person === null || !isset($this->schedule->family($principal->personId)[$person])
                || !$this->store->lock($observed['lessonId'])
            ) {
                throw self::notFound();
            }

            // A competing cancellation, reschedule or change to the family may have come first.
            $booking = $this->store->booking($id);
            $lesson = $this->schedule->lesson($observed['lessonId']);
            $name = $this->schedule->family($principal->personId)[$person] ?? null;
            if ($booking === null || $booking['personId'] !== $person || $booking['lessonId'] !== $observed['lessonId']
                || $lesson === null || $name === null
            ) {
                throw self::notFound();
            }
            $now = $this->clock->now();
            if ($booking['status'] === 'canceled') {
                throw new CatalogFailure(409, 'the booking is canceled already', reason: 'already-canceled');
            }
            if ($now >= $lesson->startsAt) {
                throw new CatalogFailure(422, 'the lesson has started', 'path.bookingId');
            }
            if ($now < new DateTimeImmutable($booking['bookedAt'])) {
                throw new CatalogFailure(422, 'validation failed: booking cannot be canceled before it was made');
            }

            $late = $lesson->status !== 'canceled' && $now >= $lesson->startsAt->modify('-' . $rules->cancellationCutoffMicros . ' microseconds');
            $this->store->cancelLocked($id, $now, $late);
            if ($lesson->status !== 'canceled' && $booking['source'] !== 'member' && !$booking['trial']) {
                $day = (string) Date::fromInstant($lesson->startsAt, $rules->timezone);
                $this->settle($booking, $person, $onBooking, $late, $day, $now);
            }
            $this->store->record($id, $principal->impersonatorId ?? $principal->accountId, 'canceled', $now, $late);

            $settings = $this->messages->settings();
            // A new invitation uses the current booking cutoff under the lesson lock.
            $this->offers->offerLocked($lesson, $this->schedule->rules(), $settings, $now);
            $this->notify($principal, $name, $lesson, $late, $now, $settings);

            return [
                'id' => $id, 'lessonId' => $lesson->id, 'personId' => $person, 'status' => 'canceled',
                'source' => $booking['source'], 'bookedAt' => $booking['bookedAt'],
                'canceledAt' => Instant::format($now), 'late' => $late,
            ];
        });
        $this->wakeMail?->__invoke();

        return $result;
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

    /** @param Settings $settings */
    private function notify(
        SessionPrincipal $principal,
        string $participant,
        PublishedLesson $lesson,
        bool $late,
        DateTimeImmutable $now,
        array $settings,
    ): void {
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
            'Lessons' => [[
                'CourseType' => $lesson->courseType['name'] ?? '',
                'StartsAt' => $lesson->startsAt->setTimezone($settings['timezone']),
                'EndsAt' => $lesson->endsAt->setTimezone($settings['timezone']),
                'Room' => $lesson->room['name'], 'Location' => $lesson->location['name'], 'Late' => $late,
            ]],
        ]);
        $this->messages->enqueueMail($recipient['email'], 'booking-canceled', $rendered, $now);
    }

    private static function notFound(): CatalogFailure
    {
        return new CatalogFailure(404, 'resource not found');
    }
}
