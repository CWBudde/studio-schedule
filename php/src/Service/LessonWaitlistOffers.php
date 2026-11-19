<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use DateTimeZone;
use RuntimeException;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Domain\ScheduleRules;
use StudioSchedule\Mail\Renderer;

/** @phpstan-import-type Settings from EventWaitlistPersistence */
final readonly class LessonWaitlistOffers
{
    /**
     * @param Closure():string $baseURL
     * @param Closure():bool $pushEnabled
     */
    public function __construct(
        private SchedulePersistence $schedule,
        private LessonWritePersistence $writes,
        private EventWaitlistPersistence $messages,
        private Renderer $mail,
        private Closure $baseURL,
        private Closure $pushEnabled,
    ) {}

    /** @param Settings $settings */
    public function offerLocked(PublishedLesson $lesson, ScheduleRules $rules, array $settings, DateTimeImmutable $now): bool
    {
        $closes = $lesson->startsAt->modify('-' . $rules->bookingCutoffMicros . ' microseconds');
        if ($lesson->status !== 'scheduled' || $lesson->kind === 'rental' || $lesson->visibility === 'hidden'
            || $lesson->startsAt <= $now || $closes <= $now) {
            return false;
        }

        $entries = $this->schedule->waitlist([$lesson->id])[$lesson->id] ?? [];
        $waiting = array_filter($entries, static fn(array $entry): bool => $entry['status'] === 'waiting');
        if ($waiting === []) {
            return false;
        }
        $active = $this->schedule->activeBookings([$lesson->id])[$lesson->id] ?? 0;
        $free = $lesson->capacity - $active - ScheduleService::heldPlaces($entries, $now);
        if ($free <= 0) {
            return false;
        }

        $contacts = $this->messages->recipients(array_values(array_unique(array_column($waiting, 'personId'))));
        $expires = min($closes, $now->setTimezone(new DateTimeZone('UTC'))->modify('+' . $settings['inviteMicros'] . ' microseconds'));
        $queued = false;
        foreach ($waiting as $entry) {
            $people = $contacts[$entry['personId']] ?? [];
            if ($people === []) {
                continue;
            }
            $token = rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
            $link = ($this->baseURL)() . '/waitlist/accept?token=' . rawurlencode($token);
            $this->writes->inviteLocked($entry['id'], hash('sha256', $token), $now, $expires);
            foreach ($people as $person) {
                if (!SetupInput::validEmail($person['email'])) {
                    throw new RuntimeException('A lesson waitlist recipient has an invalid email address');
                }
                $locale = $this->mail->resolveLocale($person['locale'] === '' ? $settings['locale'] : $person['locale']);
                $data = [
                    'SchoolName' => $settings['name'], 'GivenName' => $person['givenName'], 'Participant' => $person['participant'],
                    'CourseType' => $lesson->courseType['name'] ?? '', 'Room' => $lesson->room['name'], 'Location' => $lesson->location['name'],
                    'StartsAt' => $lesson->startsAt->setTimezone($settings['timezone']), 'EndsAt' => $lesson->endsAt->setTimezone($settings['timezone']),
                    'ExpiresAt' => $expires->setTimezone($settings['timezone']), 'Link' => $link,
                ];
                $rendered = $this->mail->render($locale, 'waitlist-invite', $data);
                $this->messages->enqueueMail($person['email'], 'waitlist-invite', $rendered, $now);
                if ($person['accountId'] !== null && ($this->pushEnabled)()) {
                    $this->push($person['accountId'], $entry['id'], $locale, $link, $data, $now, $expires);
                }
            }
            $queued = true;
            if (--$free === 0) {
                break;
            }
        }

        return $queued;
    }

    /** @param array<string,mixed> $data */
    private function push(string $account, string $entry, string $locale, string $link, array $data, DateTimeImmutable $now, DateTimeImmutable $expires): void
    {
        $subscriptions = $this->messages->pushSubscriptions($account);
        if ($subscriptions === []) {
            return;
        }
        $text = $this->mail->renderPush($locale, 'waitlist-invite', $data);
        $payload = json_encode(
            $text + ['url' => $link, 'tag' => 'waitlist-invite-' . $entry, 'lang' => $locale],
            JSON_THROW_ON_ERROR | JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES | JSON_HEX_TAG | JSON_HEX_AMP,
        );
        if (strlen($payload) > 3000) {
            throw new RuntimeException('The lesson waitlist notification is too large');
        }
        $this->messages->enqueuePush($subscriptions, $payload, $now, $expires);
    }
}
