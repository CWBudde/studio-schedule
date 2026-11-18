<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use SensitiveParameter;
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
        DateTimeImmutable $now,
    ): void {
        $postgres = $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $uuid = Uuid::v7();
        $id = $postgres ? $uuid->toRfc4122() : $uuid->toBinary();
        $owner = $postgres ? $accountId : Uuid::fromString($accountId)->toBinary();
        $instant = $now->setTimezone(new DateTimeZone('UTC'))->format($postgres ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u');
        $sql = 'INSERT INTO push_subscriptions (id, account_id, endpoint, p256dh, auth, device_label, created_at, last_used_at) '
            . 'VALUES (?, ?, ?, ?, ?, ?, ?, ?)';
        $idType = $postgres ? ParameterType::STRING : ParameterType::BINARY;
        $refresh = [$publicKey, $auth, $label, $instant];
        $refreshTypes = [ParameterType::BINARY, ParameterType::BINARY, $label === null ? ParameterType::NULL : ParameterType::STRING, ParameterType::STRING];
        $arguments = [$id, $owner, $endpoint, ...$refresh, $instant];
        $types = [$idType, $idType, ParameterType::STRING, ...$refreshTypes, ParameterType::STRING];

        $this->database->transactional(static function (Connection $database) use ($postgres, $sql, $arguments, $types, $endpoint, $id, $owner, $idType, $refresh, $refreshTypes): void {
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
                return;
            }
            if ($saved['account_id'] !== $owner) {
                // The old subscription's queued messages are deleted by the foreign key.
                $database->executeStatement('DELETE FROM push_subscriptions WHERE id = ?', [$saved['id']], [$idType]);
                $database->executeStatement($sql, $arguments, $types);

                return;
            }

            $database->executeStatement(
                'UPDATE push_subscriptions SET p256dh = ?, auth = ?, device_label = ?, last_used_at = GREATEST(last_used_at, ?) WHERE id = ?',
                [...$refresh, $saved['id']],
                [...$refreshTypes, $idType],
            );
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
