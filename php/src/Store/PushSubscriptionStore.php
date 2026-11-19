<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateInterval;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use Psr\Clock\ClockInterface;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Service\SessionRecord;
use Symfony\Component\Uid\Uuid;

final readonly class PushSubscriptionStore
{
    public function __construct(private Connection $database) {}

    public function save(
        string $accountId,
        #[SensitiveParameter]
        string $endpoint,
        #[SensitiveParameter]
        string $publicKey,
        #[SensitiveParameter]
        string $auth,
        #[SensitiveParameter]
        ?string $label,
        ClockInterface $clock,
        #[SensitiveParameter]
        string $sessionHash,
    ): bool {
        $postgres = $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $uuid = Uuid::v7();
        $id = $postgres ? $uuid->toRfc4122() : $uuid->toBinary();
        $owner = $postgres ? $accountId : Uuid::fromString($accountId)->toBinary();
        $idType = $postgres ? ParameterType::STRING : ParameterType::BINARY;
        $database = $this->database;

        return Database::retryWrite($database, static function () use ($database, $postgres, $owner, $idType, $sessionHash, $clock, $id, $endpoint, $publicKey, $auth, $label): bool {
            // Password changes and account administration lock the account before revoking sessions and push subscriptions.
            if ($database->fetchOne(
                'SELECT id FROM accounts WHERE id = ? AND confirmed_at IS NOT NULL AND disabled_at IS NULL FOR UPDATE',
                [$owner],
                [$idType],
            ) === false) {
                return false;
            }
            $now = $clock->now()->setTimezone(new DateTimeZone('UTC'));
            $instant = Database::sqlInstant($database, $now);
            $cutoff = Database::sqlInstant($database, $now->sub(new DateInterval(SessionRecord::MAX_AGE)));
            $digest = $postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
            if ($database->fetchOne(
                "SELECT id FROM sessions WHERE account_id = ? AND token_hash = $digest AND impersonator_id IS NULL "
                . 'AND expires_at > ? AND created_at > ? FOR UPDATE',
                [$owner, $sessionHash, $instant, $cutoff],
                [$idType],
            ) === false) {
                return false;
            }

            $sql = 'INSERT INTO push_subscriptions (id, account_id, endpoint, p256dh, auth, device_label, created_at, last_used_at) '
                . 'VALUES (?, ?, ?, ?, ?, ?, ?, ?)';
            $refresh = [$publicKey, $auth, $label, $instant];
            $refreshTypes = [ParameterType::BINARY, ParameterType::BINARY, $label === null ? ParameterType::NULL : ParameterType::STRING, ParameterType::STRING];
            $arguments = [$id, $owner, $endpoint, ...$refresh, $instant];
            $types = [$idType, $idType, ParameterType::STRING, ...$refreshTypes, ParameterType::STRING];
            // Let the unique index serialize saves even when the endpoint did not exist at the start.
            $onConflict = $postgres ? ' ON CONFLICT (endpoint) DO UPDATE SET id = push_subscriptions.id'
                : ' ON DUPLICATE KEY UPDATE id = push_subscriptions.id';
            $database->executeStatement($sql . $onConflict, $arguments, $types);
            /** @var array{id:string,account_id:string}|false $saved */
            $saved = $database->fetchAssociative('SELECT id, account_id FROM push_subscriptions WHERE endpoint = ? FOR UPDATE', [$endpoint]);
            if ($saved === false) {
                throw new RuntimeException('saved push subscription not found');
            }
            if ($saved['id'] === $id) {
                return true;
            }
            if ($saved['account_id'] !== $owner) {
                // The old subscription's queued messages are deleted by the foreign key.
                $database->executeStatement('DELETE FROM push_subscriptions WHERE id = ?', [$saved['id']], [$idType]);
                $database->executeStatement($sql, $arguments, $types);

                return true;
            }

            $database->executeStatement(
                'UPDATE push_subscriptions SET p256dh = ?, auth = ?, device_label = ?, last_used_at = GREATEST(last_used_at, ?) WHERE id = ?',
                [...$refresh, $saved['id']],
                [...$refreshTypes, $idType],
            );

            return true;
        });
    }

    public function delete(string $accountId, #[SensitiveParameter] string $endpoint): void
    {
        if (strlen($endpoint) > 2048 || str_contains($endpoint, "\0")) {
            return;
        }
        $postgres = $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $this->database->executeStatement(
            'DELETE FROM push_subscriptions WHERE endpoint = ? AND account_id = ?',
            [$endpoint, $postgres ? $accountId : Uuid::fromString($accountId)->toBinary()],
            [ParameterType::STRING, $postgres ? ParameterType::STRING : ParameterType::BINARY],
        );
    }
}
