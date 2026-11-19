<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\Event;
use StudioSchedule\Domain\Money;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Mail\Renderer;
use Throwable;

/**
 * @phpstan-import-type Entry from EventWaitlistPersistence
 * @phpstan-import-type Recipient from EventWaitlistPersistence
 * @phpstan-import-type Settings from EventWaitlistPersistence
 */
final readonly class EventWaitlist
{
    private const int MAX_PUSH_BYTES = 3000;

    public function __construct(
        private EventPersistence $events,
        private EventWaitlistPersistence $store,
        private ClockInterface $clock,
        private Renderer $mail,
        private string $baseURL,
        private bool $pushEnabled,
    ) {}

    public function expireDue(): int
    {
        $total = 0;
        $failure = null;
        foreach ($this->store->dueEvents($this->clock->now()) as $id) {
            try {
                $total += $this->events->write(function () use ($id): int {
                    $event = $this->events->lockEvent($id) ?? throw new RuntimeException('The event with a due offer is missing');

                    return $this->settleLocked($event, $this->clock->now());
                });
            } catch (SettingsNotFound) {
                // A school that has not been set up yet has no expiry work.
                continue;
            } catch (Throwable $error) {
                // Failure in one event must not roll back successful work in another event.
                $failure ??= $error;
            }
        }
        if ($failure !== null) {
            throw $failure;
        }

        return $total;
    }

    public function respondByToken(#[SensitiveParameter] string $token, bool $accept): ?true
    {
        $hash = hash('sha256', $token);

        return $this->events->write(function () use ($hash, $accept): ?true {
            $observed = $this->store->byTokenHash($hash);
            if ($observed === null) {
                return null;
            }
            $event = $this->events->lockEvent($observed['eventId']);
            $offer = $this->store->byTokenHash($hash);
            if ($event === null || $offer === null || $offer['id'] !== $observed['id']) {
                return null;
            }

            $now = $this->clock->now();
            if ($offer['expiresAt'] <= $now) {
                // Commit the expiry and any new offer even though this link returns 404.
                $this->settleLocked($event, $now);

                return null;
            }
            if (!$accept) {
                $this->finishLocked($event, $offer['id'], $now, accept: false, held: true);

                return true;
            }

            $sessions = $this->events->sessions([$event->id]);
            [, $cancelUntil, $open] = EventService::signupWindow($event, $sessions, $now);
            $person = $offer['personId'];
            if (!$open || $person === null || !$this->store->lockLivePerson($person)) {
                return null;
            }

            $this->finishLocked($event, $offer['id'], $now, accept: true, held: true);
            $contacts = $this->store->recipients([$person])[$person] ?? [];
            $this->sendConfirmation($event, $offer['payment'], $offer['price'], $sessions, $cancelUntil, $now, $contacts);

            return true;
        });
    }

    /** End expired offers and offer the places again while the caller holds the event lock. */
    public function settleLocked(Event $event, DateTimeImmutable $now): int
    {
        $due = $this->store->dueInvites($event->id, $now);
        if ($due === []) {
            return 0;
        }
        $settings = $this->store->settings();
        $recipients = $this->recipients($due);
        foreach ($due as $entry) {
            $this->store->finishInvite($entry['id'], $now, accept: false);
            foreach ($recipients[$entry['personId'] ?? ''] ?? [] as $recipient) {
                $this->notify($recipient, $settings, $event, $now, $now, link: null);
            }
        }
        $this->offerLocked($event, $now, $settings);

        return count($due);
    }

    public function finishLocked(Event $event, string $id, DateTimeImmutable $now, bool $accept, bool $held): void
    {
        $this->store->finishInvite($id, $now, $accept);
        if (!$accept && $held) {
            $this->offerLocked($event, $now);
        }
    }

    /**
     * Offer every open place the event can fill while the caller holds its lock.
     *
     * @param Settings|null $settings
     */
    public function offerLocked(Event $event, DateTimeImmutable $now, ?array $settings = null): void
    {
        if ($event->status !== 'published') {
            return;
        }
        $counts = $this->events->registrationCounts([$event->id], $now)[$event->id]
            ?? ['registered' => 0, 'invited' => 0, 'waiting' => 0];
        $free = $event->capacity - $counts['registered'] - $counts['invited'];
        if ($free <= 0 || $counts['waiting'] === 0) {
            return;
        }

        [$closes, , $open] = EventService::signupWindow($event, $this->events->sessions([$event->id]), $now);
        if (!$open || $closes === null) {
            return;
        }
        $settings ??= $this->store->settings();
        $expiry = $now->setTimezone(new DateTimeZone('UTC'))->modify('+' . $settings['inviteMicros'] . ' microseconds');
        $expires = min($expiry, $closes);
        $waiting = $this->store->waiting($event->id);
        $recipients = $this->recipients($waiting);
        foreach ($waiting as $entry) {
            $contacts = $recipients[$entry['personId'] ?? ''] ?? [];
            if ($contacts === []) {
                continue;
            }
            $token = rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
            $this->store->invite($entry['id'], hash('sha256', $token), $now, $expires);
            $link = $this->baseURL . '/events/invite?token=' . rawurlencode($token);
            foreach ($contacts as $recipient) {
                $this->notify($recipient, $settings, $event, $now, $expires, $link);
            }
            if (--$free === 0) {
                break;
            }
        }
    }

    /**
     * @param list<PublishedLesson> $sessions
     * @param list<Recipient> $recipients
     */
    public function sendConfirmation(
        Event $event,
        string $payment,
        int $price,
        array $sessions,
        ?DateTimeImmutable $cancelUntil,
        DateTimeImmutable $now,
        #[SensitiveParameter]
        array $recipients,
    ): void {
        $settings = $this->store->settings();
        $timezone = $settings['timezone'];
        $slots = [];
        foreach ($sessions as $session) {
            if ($session->status === 'scheduled') {
                $slots[] = [
                    'CourseType' => $event->courseType['name'], 'Room' => $session->room['name'],
                    'Location' => $session->location['name'], 'StartsAt' => $session->startsAt->setTimezone($timezone),
                    'EndsAt' => $session->endsAt->setTimezone($timezone),
                ];
            }
        }
        foreach ($recipients as $recipient) {
            $locale = $this->locale($recipient, $settings);
            $data = $this->mailData($recipient, $settings, $event) + [
                'Sessions' => $slots, 'CancelUntil' => $cancelUntil?->setTimezone($timezone),
                'Fee' => $payment === 'unpaid' ? (new Money($price))->format($settings['currency'], $locale) : '',
            ];
            $this->queueMail($recipient['email'], $locale, 'event-registered', $data, $now);
        }
    }

    /** @param Recipient $recipient */
    public function sendCancellation(Event $event, DateTimeImmutable $now, #[SensitiveParameter] array $recipient, bool $refunded): void
    {
        $settings = $this->store->settings();
        $this->queueMail(
            $recipient['email'],
            $this->locale($recipient, $settings),
            'event-registration-canceled',
            $this->mailData($recipient, $settings, $event) + ['Refunded' => $refunded],
            $now,
        );
    }

    /**
     * @param list<Entry> $entries
     * @return array<string,list<Recipient>>
     */
    private function recipients(array $entries): array
    {
        $ids = array_values(array_filter(array_column($entries, 'personId'), is_string(...)));

        return $this->store->recipients($ids);
    }

    /**
     * @param Recipient $recipient
     * @param Settings $settings
     */
    private function notify(
        #[SensitiveParameter]
        array $recipient,
        array $settings,
        Event $event,
        DateTimeImmutable $now,
        DateTimeImmutable $expires,
        #[SensitiveParameter]
        ?string $link,
    ): void {
        $locale = $this->locale($recipient, $settings);
        $data = $this->mailData($recipient, $settings, $event) + [
            'ExpiresAt' => $expires->setTimezone($settings['timezone']),
            'Link' => $link ?? '',
        ];
        $template = $link === null ? 'event-invite-expired' : 'event-invite';
        $this->queueMail($recipient['email'], $locale, $template, $data, $now);
        if (!$this->pushEnabled || $link === null || $recipient['accountId'] === null) {
            return;
        }

        $subscriptions = $this->store->pushSubscriptions($recipient['accountId']);
        if ($subscriptions === []) {
            return;
        }
        $text = $this->mail->renderPush($locale, $template, $data);
        $payload = json_encode(
            $text + ['url' => $link, 'tag' => 'event-invite-' . $event->id, 'lang' => $locale],
            JSON_THROW_ON_ERROR | JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES | JSON_HEX_TAG | JSON_HEX_AMP,
        );
        if (strlen($payload) > self::MAX_PUSH_BYTES) {
            throw new RuntimeException('The event waitlist notification is too large');
        }
        $this->store->enqueuePush($subscriptions, $payload, $now, $expires);
    }

    /**
     * @param Recipient $recipient
     * @param Settings $settings
     */
    private function locale(#[SensitiveParameter] array $recipient, array $settings): string
    {
        return $this->mail->resolveLocale($recipient['locale'] === '' ? $settings['locale'] : $recipient['locale']);
    }

    /**
     * @param Recipient $recipient
     * @param Settings $settings
     * @return array<string,string>
     */
    private function mailData(#[SensitiveParameter] array $recipient, array $settings, Event $event): array
    {
        return [
            'SchoolName' => $settings['name'], 'GivenName' => $recipient['givenName'], 'Title' => $event->title,
            'Participant' => $recipient['participant'],
        ];
    }

    /** @param array<string,mixed> $data */
    private function queueMail(
        #[SensitiveParameter]
        string $email,
        string $locale,
        string $template,
        #[SensitiveParameter]
        array $data,
        DateTimeImmutable $now,
    ): void {
        if (!SetupInput::validEmail($email)) {
            throw new RuntimeException('The event waitlist recipient has an invalid email address');
        }
        $this->store->enqueueMail($email, $template, $this->mail->render($locale, $template, $data), $now);
    }
}
