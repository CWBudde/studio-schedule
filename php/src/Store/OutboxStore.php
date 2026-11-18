<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateInterval;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-type OutboxClaim array{id:string,recipient:string,subject:string,text:?string,html:string,replyTo:?string,receiptId:?string,attempt:int,leaseUntil:DateTimeImmutable}
 */
final readonly class OutboxStore
{
    private const int MAX_ATTEMPTS = 5;
    private const array RETRY_DELAYS = ['PT5M', 'PT15M', 'PT1H', 'PT3H'];

    public function __construct(private Connection $connection) {}

    public function retireExpiredLastAttempts(DateTimeImmutable $now): void
    {
        // A crashed worker may leave its final attempt leased but unrecorded.
        $this->connection->executeStatement(
            'UPDATE outbox_mails SET next_attempt_at = NULL, last_error = ? '
            . 'WHERE sent_at IS NULL AND next_attempt_at <= ? AND attempts >= ?',
            ['Final delivery attempt expired', $this->timestamp($now), self::MAX_ATTEMPTS],
        );
    }

    /** @return OutboxClaim|null */
    public function claim(DateTimeImmutable $now): ?array
    {
        return $this->connection->transactional(function (Connection $connection) use ($now): ?array {
            $row = $connection->fetchAssociative(
                'SELECT id, recipient, subject, body_text, body_html, reply_to, receipt_id, attempts FROM outbox_mails '
                . 'WHERE sent_at IS NULL AND next_attempt_at <= ? AND attempts < ? '
                . 'ORDER BY next_attempt_at, id LIMIT 1 FOR UPDATE SKIP LOCKED',
                [$this->timestamp($now), self::MAX_ATTEMPTS],
            );
            if ($row === false) {
                return null;
            }

            assert(is_string($row['id']) && is_string($row['recipient']) && is_string($row['subject']) && is_numeric($row['attempts']));
            assert($row['body_text'] === null || is_string($row['body_text']));
            assert($row['body_html'] === null || is_string($row['body_html']));
            assert($row['reply_to'] === null || is_string($row['reply_to']));
            assert($row['receipt_id'] === null || is_string($row['receipt_id']));

            $attempt = (int) $row['attempts'] + 1;
            $delay = self::RETRY_DELAYS[min($attempt, count(self::RETRY_DELAYS)) - 1];
            // The next retry time doubles as a lease, so the SMTP send needs no database lock.
            $leaseUntil = $now->setTimezone(new DateTimeZone('UTC'))->add(new DateInterval($delay));
            $connection->executeStatement(
                'UPDATE outbox_mails SET attempts = ?, next_attempt_at = ? WHERE id = ?',
                [$attempt, $this->timestamp($leaseUntil), $row['id']],
            );

            $receiptId = $row['receipt_id'];
            if ($receiptId !== null && !$this->isPostgres()) {
                $receiptId = Uuid::fromBinary($receiptId)->toRfc4122();
            }

            return [
                'id' => $row['id'],
                'recipient' => $row['recipient'],
                'subject' => $row['subject'],
                'text' => $row['body_text'],
                'html' => $row['body_html'] ?? '',
                'replyTo' => $row['reply_to'],
                'receiptId' => $receiptId,
                'attempt' => $attempt,
                'leaseUntil' => $leaseUntil,
            ];
        });
    }

    /**
     * Atomically record the outcome only while this claim is still current.
     *
     * @param OutboxClaim $claim
     */
    public function record(#[\SensitiveParameter] array $claim, DateTimeImmutable $now, ?string $error, bool $permanent): bool
    {
        $lease = $this->timestamp($claim['leaseUntil']);
        $sentAt = $error === null ? $this->timestamp($now) : null;
        $nextAttempt = $error === null || $permanent || $claim['attempt'] >= self::MAX_ATTEMPTS ? null : $lease;

        return $this->connection->executeStatement(
            'UPDATE outbox_mails SET sent_at = ?, next_attempt_at = ?, last_error = ? '
            . 'WHERE id = ? AND sent_at IS NULL AND attempts = ? AND next_attempt_at = ?',
            [$sentAt, $nextAttempt, $error, $claim['id'], $claim['attempt'], $lease],
        ) === 1;
    }

    private function timestamp(DateTimeImmutable $instant): string
    {
        return $instant->setTimezone(new DateTimeZone('UTC'))->format($this->isPostgres() ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u');
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
