<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Psr\Clock\ClockInterface;

final readonly class ReadinessCache
{
    private const string READ_CACHE = 'SELECT version_hash, checked_at_us, ready FROM http_readiness_cache WHERE id = 1';

    public function __construct(
        private Connection $connection,
        private ClockInterface $clock,
        private int $ttlSeconds,
    ) {}

    /** @param Closure():bool $probe */
    public function remember(string $versionHash, Closure $probe): bool
    {
        $cached = $this->fresh($this->connection->fetchAssociative(self::READ_CACHE), $versionHash);

        return $cached ?? $this->connection->transactional(function (Connection $connection) use ($versionHash, $probe): bool {
            // Recheck after taking the row lock so one worker refreshes the shared entry.
            $cached = $this->fresh($connection->fetchAssociative(self::READ_CACHE . ' FOR UPDATE'), $versionHash);
            if ($cached !== null) {
                return $cached;
            }

            $ready = $probe();
            $connection->executeStatement(
                'UPDATE http_readiness_cache SET version_hash = ?, checked_at_us = ?, ready = ? WHERE id = 1',
                [$versionHash, $this->nowMicros(), $ready],
                [ParameterType::STRING, ParameterType::INTEGER, ParameterType::BOOLEAN],
            );

            return $ready;
        });
    }

    /** @param array<string,mixed>|false $row */
    private function fresh(array|false $row, string $versionHash): ?bool
    {
        if ($row === false || $row['version_hash'] !== $versionHash || !is_numeric($row['checked_at_us'])) {
            return null;
        }

        $age = $this->nowMicros() - (int) $row['checked_at_us'];
        if ($age < 0 || $age >= $this->ttlSeconds * 1_000_000) {
            return null;
        }

        return in_array($row['ready'], [true, 1, '1', 't', 'true'], true);
    }

    private function nowMicros(): int
    {
        $instant = $this->clock->now();

        return $instant->getTimestamp() * 1_000_000 + (int) $instant->format('u');
    }
}
