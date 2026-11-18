<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateInterval;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use SensitiveParameter;

/**
 * @phpstan-type Subscription array{account_id:string,endpoint:string,public_key:string,auth_key:string,last_used_at:string}
 * @phpstan-type PushClaim array{
 *     id:string,subscriptionId:string,subscription:Subscription,payload:string,attempt:int,
 *     expiresAt:DateTimeImmutable,leaseUntil:DateTimeImmutable,ttl:int
 * }
 */
final readonly class PushOutboxStore
{
    private const int MAX_ATTEMPTS = 5;
    private const int MAX_TTL_SECONDS = 604800;
    private const array RETRY_DELAYS = ['PT1M', 'PT5M', 'PT15M', 'PT1H'];

    public function __construct(private Connection $database) {}

    /** @return PushClaim|array{discarded:true}|null */
    public function claim(DateTimeImmutable $now): ?array
    {
        $now = $now->setTimezone(new DateTimeZone('UTC'));

        return $this->database->transactional(function (Connection $database) use ($now): ?array {
            $expiryColumn = $this->utcTimestampColumn('expires_at');
            /** @var array{id:string,subscription_id:string,payload:string,expires_at:string,attempts:int|numeric-string}|false $row */
            $row = $database->fetchAssociative(
                "SELECT id, subscription_id, payload, $expiryColumn, attempts FROM push_outbox "
                . 'WHERE next_attempt_at <= ? ORDER BY next_attempt_at, id LIMIT 1 FOR UPDATE SKIP LOCKED',
                [$this->timestamp($now)],
            );
            if ($row === false) {
                return null;
            }

            $expiry = new DateTimeImmutable($row['expires_at'], new DateTimeZone('UTC'));
            $attempts = (int) $row['attempts'];
            if ($expiry <= $now || $attempts >= self::MAX_ATTEMPTS) {
                $database->executeStatement(
                    'UPDATE push_outbox SET failed_at = ?, next_attempt_at = NULL, last_error = ? WHERE id = ?',
                    [$this->timestamp($now), $expiry <= $now ? 'expired before delivery' : 'Final delivery attempt expired', $row['id']],
                );

                return ['discarded' => true];
            }

            $subscription = $this->subscription($row['subscription_id']);
            if ($subscription === null) {
                // The missing parent normally deletes the message through its foreign key.
                $database->executeStatement(
                    'UPDATE push_outbox SET failed_at = ?, next_attempt_at = NULL, last_error = ? WHERE id = ?',
                    [$this->timestamp($now), 'Push subscription is missing', $row['id']],
                );

                return ['discarded' => true];
            }

            $lease = $now->add(new DateInterval(self::RETRY_DELAYS[min($attempts, count(self::RETRY_DELAYS) - 1)]));
            $attempt = $attempts + 1;
            $database->executeStatement(
                'UPDATE push_outbox SET attempts = ?, next_attempt_at = ? WHERE id = ?',
                [$attempt, $this->timestamp($lease), $row['id']],
            );
            $ttl = $expiry->getTimestamp() - $now->getTimestamp();
            if ($expiry->format('u') < $now->format('u')) {
                --$ttl;
            }

            return [
                'id' => $row['id'],
                'subscriptionId' => $row['subscription_id'],
                'subscription' => $subscription,
                'payload' => $row['payload'],
                'attempt' => $attempt,
                'expiresAt' => $expiry,
                'leaseUntil' => $lease,
                'ttl' => min($ttl, self::MAX_TTL_SECONDS),
            ];
        });
    }

    /** @param PushClaim $claim */
    public function record(
        #[SensitiveParameter]
        array $claim,
        DateTimeImmutable $now,
        ?string $error,
        bool $permanent = false,
        bool $gone = false,
    ): bool {
        return $this->database->transactional(function (Connection $database) use ($claim, $now, $error, $permanent, $gone): bool {
            // Lock the subscription before touching its outbox, in the same order as deletion.
            $subscription = $this->subscription($claim['subscriptionId'], lock: true);
            if ($subscription === null) {
                return false;
            }
            $sameSubscription = $subscription === $claim['subscription'];
            if ($subscription['account_id'] !== $claim['subscription']['account_id']) {
                $error = 'Push subscription changed account during delivery';
                $permanent = true;
                $gone = false;
            } elseif (!$sameSubscription && $error !== null) {
                // A refreshed device must not be removed or given up on an old answer.
                $error = 'Push subscription refreshed during delivery';
                $permanent = false;
                $gone = false;
            }

            $instant = $this->timestamp($now);
            $lease = $this->timestamp($claim['leaseUntil']);
            $sent = $error === null;
            $failed = !$sent && ($permanent || $claim['attempt'] >= self::MAX_ATTEMPTS
                || $claim['leaseUntil'] >= $claim['expiresAt'] || $now >= $claim['expiresAt']);
            $changed = $database->executeStatement(
                'UPDATE push_outbox SET sent_at = ?, failed_at = ?, next_attempt_at = ?, last_error = ? '
                . 'WHERE id = ? AND subscription_id = ? AND attempts = ? AND next_attempt_at = ?',
                [$sent ? $instant : null, $failed ? $instant : null, $sent || $failed ? null : $lease, $error,
                    $claim['id'], $claim['subscriptionId'], $claim['attempt'], $lease],
            );
            if ($changed !== 1) {
                return false;
            }

            if ($gone) {
                $database->executeStatement('DELETE FROM push_subscriptions WHERE id = ?', [$claim['subscriptionId']]);
            } elseif ($sent && $sameSubscription) {
                $database->executeStatement(
                    'UPDATE push_subscriptions SET last_used_at = ? WHERE id = ? AND last_used_at < ?',
                    [$instant, $claim['subscriptionId'], $instant],
                );
            }

            return $sent;
        });
    }

    /** @return Subscription|null */
    private function subscription(string $id, bool $lock = false): ?array
    {
        $keys = $this->isPostgres() ? "encode(p256dh, 'hex') AS public_key, encode(auth, 'hex') AS auth_key"
            : 'HEX(p256dh) AS public_key, HEX(auth) AS auth_key';
        $lastUsed = $this->utcTimestampColumn('last_used_at');
        /** @var Subscription|false $row */
        $row = $this->database->fetchAssociative(
            "SELECT account_id, endpoint, $keys, $lastUsed FROM push_subscriptions WHERE id = ?" . ($lock ? ' FOR UPDATE' : ''),
            [$id],
        );

        return $row === false ? null : $row;
    }

    /** @param 'expires_at'|'last_used_at' $column */
    private function utcTimestampColumn(string $column): string
    {
        // PDO otherwise reads PostgreSQL timestamp text in the session's DateStyle and time zone.
        return $this->isPostgres()
            ? "TO_CHAR($column AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS.US') AS $column"
            : $column;
    }

    private function timestamp(DateTimeImmutable $instant): string
    {
        return $instant->setTimezone(new DateTimeZone('UTC'))->format($this->isPostgres() ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u');
    }

    private function isPostgres(): bool
    {
        return $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
