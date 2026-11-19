<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateInterval;
use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Domain\TimeOfDay;
use StudioSchedule\Mail\Formatting;
use StudioSchedule\Mail\Renderer;

final readonly class LessonNotices
{
    private const int PER_HOUR = 5;
    private const int PER_LESSON_DAY = 3;

    /** @param (Closure():void)|null $wakeMail */
    public function __construct(
        private LessonNoticePersistence $store,
        private EventWaitlistPersistence $school,
        private LessonRecipients $recipients,
        private Renderer $mail,
        private ClockInterface $clock,
        private ?Closure $wakeMail = null,
    ) {}

    public function send(string $id, SessionPrincipal $principal, #[SensitiveParameter] string $message): int
    {
        $message = self::plainText($message);
        $count = $this->store->write(function () use ($id, $principal, $message): int {
            $settings = $this->school->settings();
            $lesson = $this->store->lockLesson($id);
            if ($lesson === null || ($principal->role === 'instructor' && $this->staffName($lesson, $principal->personId) === null)) {
                throw new CatalogFailure(404, 'resource not found');
            }

            $actor = $principal->impersonatorId ?? $principal->accountId;
            $sender = $this->store->lockSender($actor);
            if ($sender === null) {
                throw new CatalogFailure(404, 'resource not found');
            }

            $now = $this->clock->now();
            $zone = $settings['timezone'];
            $this->requireQuota($lesson->id, $actor, $now, $zone);
            $messages = $this->recipients->forLesson($lesson);
            if ($messages === []) {
                return 0;
            }

            $data = [
                'SchoolName' => $settings['name'], 'CourseType' => $lesson->courseType['name'] ?? '',
                'StartsAt' => $lesson->startsAt->setTimezone($zone), 'EndsAt' => $lesson->endsAt->setTimezone($zone),
                'Room' => $lesson->room['name'], 'Location' => $lesson->location['name'],
                'Canceled' => $lesson->status === 'canceled', 'Message' => $message,
                'Sender' => $this->staffName($lesson, $sender['personId']) ?? $sender['name'],
            ];
            foreach ($messages as $delivery) {
                $to = $delivery['recipient'];
                if (!SetupInput::validEmail($to['email']) || !SetupInput::validEmail($sender['email'])) {
                    throw new RuntimeException('A lesson notice has an invalid mail address');
                }
                $locale = $to['locale'] === '' ? $settings['locale'] : $to['locale'];
                $mail = $this->mail->render($locale, 'lesson-notice', $data + [
                    'GivenName' => $to['givenName'], 'Participants' => $delivery['participants'],
                ]);
                $this->store->enqueue($to['email'], $sender['email'], $mail, $now);
            }

            $count = count($messages);
            $this->store->record($lesson->id, $actor, $now, $count);

            return $count;
        });
        if ($count > 0) {
            $this->wakeMail?->__invoke();
        }

        return $count;
    }

    private function staffName(PublishedLesson $lesson, string $person): ?string
    {
        $staff = $this->store->activeStaff($person);
        foreach ([$lesson->instructor, $lesson->accompanist] as $ref) {
            if ($ref !== null && isset($staff[$ref['id']])) {
                return $ref['displayName'];
            }
        }

        return null;
    }

    private function requireQuota(string $lesson, string $actor, DateTimeImmutable $now, DateTimeZone $zone): void
    {
        $hour = new DateInterval('PT1H');
        $senderThreshold = $this->store->senderNoticeAtOffset($actor, $now->sub($hour), self::PER_HOUR - 1);
        $retryAt = $senderThreshold?->add($hour);
        $today = Date::fromInstant($now, $zone);
        if ($this->store->lessonNoticeCount($lesson, $today->at(TimeOfDay::parse('00:00'), $zone)) >= self::PER_LESSON_DAY) {
            $midnight = $today->nextMidnight($zone);
            $retryAt = $retryAt === null ? $midnight : max($retryAt, $midnight);
        }
        if ($retryAt !== null) {
            $seconds = $retryAt->getTimestamp() - $now->getTimestamp();
            $roundUp = $retryAt->format('u') > $now->format('u') ? 1 : 0;
            throw new LessonNoticeLimit(max(1, $seconds + $roundUp));
        }
    }

    private static function plainText(#[SensitiveParameter] string $message): string
    {
        $text = Formatting::trimWhitespace(str_replace(["\r\n", "\r", "\u{2028}", "\u{2029}"], "\n", $message));
        if ($text === '' || mb_strlen($text, 'UTF-8') > 1000) {
            throw CatalogFailure::field('message', 'expected 1 to 1000 characters');
        }
        if (preg_match('/(?![\n\t\x{200C}\x{200D}])[\p{Cc}\p{Cf}]/u', $text) !== 0) {
            throw CatalogFailure::field('message', 'must not contain control or invisible format characters other than line breaks and tabs');
        }

        return $text;
    }
}
