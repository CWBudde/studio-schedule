<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use DateTimeZone;
use InvalidArgumentException;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Domain\Id;
use StudioSchedule\Mail\TemplateData;
use StudioSchedule\Store\MailLogStore;

/** @phpstan-import-type MailLogRow from MailLogStore */
final readonly class MailLog
{
    private const int PAGE_SIZE = 50;

    public function __construct(private MailLogStore $store, private ClockInterface $clock) {}

    /** @return array{items:list<array<string,mixed>>, nextCursor?:string} */
    public function search(string $status, string $template, #[SensitiveParameter] string $recipient, string $cursor): array
    {
        $rows = $this->store->search($status, $template, $recipient, self::decodeCursor($cursor), self::PAGE_SIZE + 1);
        $page = ['items' => array_map(self::item(...), array_slice($rows, 0, self::PAGE_SIZE))];
        if (count($rows) > self::PAGE_SIZE) {
            $last = $rows[self::PAGE_SIZE - 1];
            $createdAt = self::dateTime($last['created_at']);
            $microseconds = $createdAt->getTimestamp() * 1_000_000 + (int) $createdAt->format('u');
            $page['nextCursor'] = rtrim(strtr(base64_encode($microseconds . '_' . $last['id']), '+/', '-_'), '=');
        }

        return $page;
    }

    /** @return array<string,mixed>|null */
    public function detail(string $id): ?array
    {
        $mail = $this->store->detail($id);

        return $mail === null ? null : self::item($mail, includeBodies: true);
    }

    /** @return array<string,mixed>|'secret-link'|'not-failed'|null */
    public function requeue(string $id, SessionPrincipal $principal): array|string|null
    {
        $mail = $this->store->requeue($id, $principal->impersonatorId ?? $principal->accountId, $this->clock->now());

        return is_array($mail) ? self::item($mail) : $mail;
    }

    /** @return array{createdAt:DateTimeImmutable,id:string}|null */
    private static function decodeCursor(string $cursor): ?array
    {
        if ($cursor === '') {
            return null;
        }
        if (preg_match('/\A[A-Za-z0-9_-]+\z/', $cursor) !== 1) {
            throw new InvalidArgumentException('invalid cursor');
        }
        $raw = base64_decode(strtr($cursor, '-_', '+/'), true);
        $parts = explode('_', $raw === false ? '' : $raw, 2);
        $id = $parts[1] ?? '';
        $micros = (int) $parts[0];
        // Bound the timestamp to the range shared by PostgreSQL and DATETIME(6).
        if (preg_match('/\A\+?[0-9]+\z/', $parts[0]) !== 1 || $micros <= 0 || $micros > 253402300799999999 || !Id::valid($id)) {
            throw new InvalidArgumentException('invalid cursor');
        }
        $createdAt = DateTimeImmutable::createFromFormat('U.u', sprintf('%d.%06d', intdiv($micros, 1_000_000), $micros % 1_000_000));
        if ($createdAt === false) {
            throw new InvalidArgumentException('invalid cursor');
        }

        return ['createdAt' => $createdAt, 'id' => strtolower($id)];
    }

    /**
     * @param MailLogRow $mail
     * @return array<string,mixed>
     */
    private static function item(#[SensitiveParameter] array $mail, bool $includeBodies = false): array
    {
        $status = match (true) {
            $mail['sent_at'] !== null => 'sent',
            $mail['next_attempt_at'] !== null => 'pending',
            default => 'failed',
        };
        $body = match (true) {
            TemplateData::secretLink($mail['template']) => 'withheld',
            in_array($mail['missing_body'], [true, 1, '1', 't'], true) => 'purged',
            default => 'shown',
        };

        return [
            'id' => $mail['id'], 'to' => $mail['recipient'], 'subject' => $mail['subject'],
            'createdAt' => self::instant($mail['created_at']), 'status' => $status, 'attempts' => (int) $mail['attempts'], 'body' => $body,
        ] + array_filter([
            'template' => $mail['template'],
            'replyTo' => $mail['reply_to'],
            'nextAttemptAt' => self::instant($mail['next_attempt_at']),
            'sentAt' => self::instant($mail['sent_at']),
            'lastError' => $mail['last_error'],
            'attachment' => $mail['receipt_number'] === null ? null : $mail['receipt_number'] . '.pdf',
            'text' => $includeBodies && $body === 'shown' ? ($mail['body_text'] ?? null) : null,
            'html' => $includeBodies && $body === 'shown' ? ($mail['body_html'] ?? null) : null,
        ], static fn(?string $field): bool => $field !== null && $field !== '');
    }

    private static function dateTime(string $value): DateTimeImmutable
    {
        $utc = new DateTimeZone('UTC');

        return (new DateTimeImmutable($value, $utc))->setTimezone($utc);
    }

    private static function instant(?string $value): ?string
    {
        if ($value === null) {
            return null;
        }
        $date = self::dateTime($value);
        $fraction = rtrim($date->format('u'), '0');

        return $date->format('Y-m-d\TH:i:s') . ($fraction === '' ? '' : '.' . $fraction) . 'Z';
    }
}
