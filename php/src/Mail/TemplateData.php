<?php

declare(strict_types=1);

namespace StudioSchedule\Mail;

use DateTimeImmutable;
use DateTimeInterface;
use InvalidArgumentException;

final class TemplateData
{
    private const array LESSON_PLACE = ['CourseType', 'Room', 'Location'];

    private const array REQUIRED_TEXT = [
        'confirm-email' => ['SchoolName'],
        'reset-password' => ['SchoolName'],
        'change-email' => ['SchoolName'],
        'email-changed' => ['SchoolName', 'NewEmail'],
        'invite' => ['SchoolName'],
        'lesson-canceled' => ['SchoolName', ...self::LESSON_PLACE],
        'lesson-restored' => ['SchoolName', ...self::LESSON_PLACE],
        'lesson-changed' => ['SchoolName', ...self::LESSON_PLACE, 'PreviousRoom', 'PreviousLocation'],
        'lesson-notice' => ['SchoolName', ...self::LESSON_PLACE, 'Sender', 'Message'],
        'booking-confirmed' => ['SchoolName', 'Participant'],
        'booking-canceled' => ['SchoolName', 'Participant'],
        'waitlist-invite' => ['SchoolName', 'Participant', ...self::LESSON_PLACE],
        'waitlist-expired' => ['SchoolName', 'Participant', ...self::LESSON_PLACE],
        'receipt' => [],
        'announcement' => ['SchoolName', 'Title', 'Message'],
        'account-deleted' => ['SchoolName'],
        'password-changed' => ['SchoolName'],
        'event-canceled' => ['SchoolName', 'Title'],
        'event-registered' => ['SchoolName', 'Title', 'Participant'],
        'event-registration-canceled' => ['SchoolName', 'Title', 'Participant'],
        'event-invite' => ['SchoolName', 'Title', 'Participant'],
        'event-invite-expired' => ['SchoolName', 'Title', 'Participant'],
        'test' => ['Server'],
    ];

    /**
     * @param array<string, mixed> $data
     * @return array<string, mixed>
     */
    public static function prepare(string $template, array $data): array
    {
        $fields = self::REQUIRED_TEXT[$template] ?? throw new InvalidArgumentException('Unknown mail template.');
        self::requireText($data, $fields);

        $data += [
            'GivenName' => '',
            'Note' => '',
            'Participants' => [],
            'Guest' => false,
            'StillWaiting' => false,
            'Canceled' => false,
            'Cancellation' => false,
            'Item' => '',
            'Refunded' => false,
            'Sessions' => [],
            'Fee' => '',
            'CancelUntil' => null,
            'FirstDay' => null,
            'LastDay' => null,
        ];

        switch ($template) {
            case 'confirm-email':
            case 'reset-password':
            case 'change-email':
            case 'invite':
                self::requireAbsoluteLink($data['Link'] ?? null);
                $hours = $data['ExpiresHours'] ?? null;
                if (!is_int($hours) || $hours <= 0) {
                    throw new InvalidArgumentException('The mail link expiry must be positive.');
                }
                break;

            case 'lesson-canceled':
            case 'lesson-restored':
            case 'lesson-changed':
            case 'lesson-notice':
                self::requireParticipants($data['Participants']);
                self::requireTimeSpan($data['StartsAt'] ?? null, $data['EndsAt'] ?? null);
                if ($template === 'lesson-changed') {
                    self::requireTimeSpan($data['PreviousStartsAt'] ?? null, $data['PreviousEndsAt'] ?? null);
                }
                break;

            case 'booking-confirmed':
            case 'booking-canceled':
                $data['Lessons'] = self::prepareSlots($data['Lessons'] ?? null, true);
                break;

            case 'waitlist-invite':
            case 'waitlist-expired':
                self::requireTimeSpan($data['StartsAt'] ?? null, $data['EndsAt'] ?? null);
                if ($template === 'waitlist-invite') {
                    self::requireAbsoluteLink($data['Link'] ?? null);
                    self::requireExpiry($data['ExpiresAt'] ?? null);
                }
                break;

            case 'event-canceled':
                self::requireParticipants($data['Participants']);
                $first = $data['FirstDay'];
                $last = $data['LastDay'];
                $data['HasDates'] = $first !== null || $last !== null;
                $data['SeveralDays'] = false;
                if ($data['HasDates']) {
                    if (!$first instanceof DateTimeInterface || !$last instanceof DateTimeInterface || $last < $first) {
                        throw new InvalidArgumentException('The event must end on or after its first session.');
                    }
                    $lastLocal = DateTimeImmutable::createFromInterface($last)->setTimezone($first->getTimezone());
                    $data['SeveralDays'] = $first->format('Y-m-d') !== $lastLocal->format('Y-m-d');
                }
                break;

            case 'event-registered':
            case 'event-registration-canceled':
                $data['Sessions'] = self::prepareSlots($data['Sessions'], false);
                break;

            case 'event-invite':
            case 'event-invite-expired':
                self::requireExpiry($data['ExpiresAt'] ?? null);
                $data += ['Link' => ''];
                break;
        }

        // ReceiptData in Go has no validator and its unspecified strings are empty.
        if ($template === 'receipt') {
            $data += ['SchoolName' => '', 'Number' => '', 'Amount' => '', 'Cancels' => ''];
        }

        return $data;
    }

    /**
     * @param array<mixed> $data
     * @param list<string> $fields
     */
    private static function requireText(array $data, array $fields): void
    {
        foreach ($fields as $field) {
            $value = $data[$field] ?? null;
            if (!is_string($value) || Formatting::trimWhitespace($value) === '') {
                throw new InvalidArgumentException("Mail field $field must not be empty.");
            }
        }
    }

    private static function requireParticipants(mixed $participants): void
    {
        if (!is_array($participants) || $participants === []) {
            throw new InvalidArgumentException('The mail needs a participant.');
        }
    }

    private static function requireTimeSpan(mixed $start, mixed $end): void
    {
        if (!$start instanceof DateTimeInterface || !$end instanceof DateTimeInterface || $end <= $start) {
            throw new InvalidArgumentException('The session must end after it starts.');
        }
    }

    private static function requireExpiry(mixed $expiry): void
    {
        if (!$expiry instanceof DateTimeInterface) {
            throw new InvalidArgumentException('The mail needs a link expiry time.');
        }
    }

    private static function requireAbsoluteLink(mixed $link): void
    {
        if (!is_string($link) || preg_match('/[\x00-\x1f\x7f]/', $link) === 1) {
            throw new InvalidArgumentException('The mail link must be an absolute HTTP(S) URL.');
        }

        $parts = parse_url($link);
        $host = $parts['host'] ?? '';
        if ($host === '' || !in_array(strtolower($parts['scheme'] ?? ''), ['http', 'https'], true)) {
            throw new InvalidArgumentException('The mail link must be an absolute HTTP(S) URL.');
        }
    }

    /** @return list<array<mixed>> */
    private static function prepareSlots(mixed $slots, bool $booking): array
    {
        if (!is_array($slots) || !array_is_list($slots) || ($booking && $slots === [])) {
            throw new InvalidArgumentException('The mail needs a list of sessions.');
        }

        $result = [];
        foreach ($slots as $slot) {
            if (!is_array($slot)) {
                throw new InvalidArgumentException('Invalid session details for the mail.');
            }

            if ($booking) {
                self::requireText($slot, self::LESSON_PLACE);
            }
            self::requireTimeSpan($slot['StartsAt'] ?? null, $slot['EndsAt'] ?? null);
            $result[] = $slot + ['Late' => false, 'Room' => '', 'Location' => ''];
        }

        return $result;
    }
}
