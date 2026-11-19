<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception as DatabaseException;
use Doctrine\DBAL\Exception\RetryableException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use InvalidArgumentException;
use Psr\Clock\ClockInterface;
use Psr\Log\LoggerInterface;
use RuntimeException;

final readonly class RateLimitStore
{
    private const int MICROS_PER_SECOND = 1_000_000;
    private const int EXPIRE_BATCH = 32;
    private const int SETTLEMENT_ATTEMPTS = 3;

    public function __construct(
        private Connection $database,
        private ClockInterface $clock,
        private ?LoggerInterface $logger = null,
    ) {}

    /** Count one request or return the seconds until the current window expires. */
    public function allow(string $bucket, #[\SensitiveParameter] string $key, int $limit, int $windowSeconds): int
    {
        [$retryAfter] = $this->admit($bucket, hash('sha256', $key), $limit, $windowSeconds, reserve: false);

        return $retryAfter;
    }

    /** Hold a slot until the caller commits or releases it, or its window expires. */
    public function reserve(string $bucket, #[\SensitiveParameter] string $key, int $limit, int $windowSeconds): RateLimitReservation|int
    {
        $keyHash = hash('sha256', $key);
        [$retryAfter, $generation] = $this->admit($bucket, $keyHash, $limit, $windowSeconds, reserve: true);
        if ($retryAfter > 0) {
            return $retryAfter;
        }

        assert(is_string($generation));

        return new RateLimitReservation(function (bool $count, bool $clear) use ($bucket, $keyHash, $generation): void {
            $this->finish($bucket, $keyHash, $generation, $count, $clear);
        });
    }

    /**
     * Check whether the live window is full without consuming an attempt.
     *
     * @phpstan-impure
     */
    public function blocked(string $bucket, #[\SensitiveParameter] string $key, int $limit): int
    {
        $digest = $this->digestSql();
        $now = $this->nowMicros();
        $expires = $this->database->fetchOne(
            "SELECT expires_at_us FROM http_rate_limits WHERE bucket = ? AND key_hash = $digest "
            . 'AND attempts + pending >= ? AND expires_at_us > ?',
            [$bucket, hash('sha256', $key), $limit, $now],
        );

        return is_numeric($expires) ? self::remaining((int) $expires, $now) : 0;
    }

    /** Clear counted attempts while preserving in-flight reservations. */
    public function forget(string $bucket, #[\SensitiveParameter] string $key): void
    {
        $digest = $this->digestSql();
        $this->database->executeStatement(
            "UPDATE http_rate_limits SET attempts = 0 WHERE bucket = ? AND key_hash = $digest AND attempts <> 0",
            [$bucket, hash('sha256', $key)],
        );
    }

    /** @return array{int, ?string} */
    private function admit(string $bucket, string $keyHash, int $limit, int $windowSeconds, bool $reserve): array
    {
        if ($limit < 1 || $windowSeconds < 1) {
            throw new InvalidArgumentException('A rate limit and its window must both be positive');
        }

        $postgres = $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $digest = $this->digestSql();
        $encode = $postgres ? "encode(generation, 'hex')" : 'LOWER(HEX(generation))';
        $nextGeneration = bin2hex(random_bytes(16));

        [$retryAfter, $generation, $startedAt] = $this->database->transactional(
            function (Connection $connection) use ($bucket, $keyHash, $limit, $reserve, $windowSeconds, $postgres, $digest, $encode, $nextGeneration): array {
                // Insert or lock the bucket before looking at its window. On
                // PostgreSQL the false predicate locks the conflict without updating it.
                $conflict = $postgres
                    ? 'ON CONFLICT (bucket, key_hash) DO UPDATE SET attempts = http_rate_limits.attempts WHERE FALSE'
                    : 'ON DUPLICATE KEY UPDATE attempts = attempts';
                $connection->executeStatement(
                    'INSERT INTO http_rate_limits (bucket, key_hash, attempts, pending, expires_at_us, generation) '
                    . "VALUES (?, $digest, 0, 0, 0, $digest) $conflict",
                    [$bucket, $keyHash, $nextGeneration],
                );

                $target = "WHERE bucket = ? AND key_hash = $digest";
                $row = $connection->fetchNumeric(
                    "SELECT attempts, pending, expires_at_us, $encode FROM http_rate_limits $target FOR UPDATE",
                    [$bucket, $keyHash],
                );
                if ($row === false || !is_numeric($row[0]) || !is_numeric($row[1]) || !is_numeric($row[2]) || !is_string($row[3])) {
                    throw new RuntimeException('Could not read the locked rate-limit window');
                }

                // A concurrent admission may have kept us waiting past expiry.
                $now = $this->nowMicros();
                $attempts = (int) $row[0];
                $pending = (int) $row[1];
                $expires = (int) $row[2];
                $generation = $row[3];
                $started = ($attempts === 0 && $pending === 0) || $expires <= $now;
                if ($started) {
                    $attempts = 0;
                    $pending = 0;
                    $expires = $now + $windowSeconds * self::MICROS_PER_SECOND;
                    $generation = $nextGeneration;
                } elseif ($attempts + $pending >= $limit) {
                    return [self::remaining($expires, $now), null, null];
                }

                $connection->executeStatement(
                    "UPDATE http_rate_limits SET attempts = ?, pending = ?, expires_at_us = ?, generation = $digest $target",
                    [$attempts + (int) !$reserve, $pending + (int) $reserve, $expires, $generation, $bucket, $keyHash],
                );

                return [0, $generation, $started ? $now : null];
            },
        );

        if ($startedAt !== null) {
            $this->expire($startedAt, $postgres, $digest);
        }

        return [$retryAfter, $generation];
    }

    private function finish(string $bucket, string $keyHash, string $generation, bool $count, bool $clear): void
    {
        $attempts = match (true) {
            $clear => '0',
            $count => 'attempts + 1',
            default => 'attempts',
        };

        for ($attempt = 1; ; ++$attempt) {
            try {
                $digest = $this->digestSql();
                $this->database->executeStatement(
                    "UPDATE http_rate_limits SET attempts = $attempts, pending = pending - 1 "
                    . "WHERE bucket = ? AND key_hash = $digest AND generation = $digest AND pending > 0 AND expires_at_us > ?",
                    [$bucket, $keyHash, $generation, $this->nowMicros()],
                );

                return;
            } catch (DatabaseException $error) {
                if ($error instanceof RetryableException && $attempt < self::SETTLEMENT_ATTEMPTS) {
                    continue;
                }

                // Leave an unresolved slot charged until expiry.
                $this->logger?->warning('Rate-limit reservation settlement failed', ['exception' => $error::class]);

                return;
            }
        }
    }

    private function digestSql(): string
    {
        return $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform ? "decode(?, 'hex')" : 'UNHEX(?)';
    }

    private function nowMicros(): int
    {
        $instant = $this->clock->now();

        return $instant->getTimestamp() * self::MICROS_PER_SECOND + (int) $instant->format('u');
    }

    private static function remaining(int $expires, int $now): int
    {
        return intdiv($expires - $now - 1, self::MICROS_PER_SECOND) + 1;
    }

    private function expire(int $now, bool $postgres, string $digest): void
    {
        // Sweep a bounded batch only as new windows open. Select without
        // locks, then delete by primary key to avoid taking range locks.
        $encode = $postgres ? "encode(key_hash, 'hex')" : 'HEX(key_hash)';
        try {
            $keys = $this->database->fetchAllNumeric(
                "SELECT bucket, $encode FROM http_rate_limits WHERE expires_at_us <= ? ORDER BY expires_at_us LIMIT " . self::EXPIRE_BATCH,
                [$now],
            );
            foreach ($keys as [$bucket, $keyHash]) {
                $this->database->executeStatement(
                    "DELETE FROM http_rate_limits WHERE bucket = ? AND key_hash = $digest AND expires_at_us <= ?",
                    [$bucket, $keyHash, $now],
                );
            }
        } catch (DatabaseException $error) {
            // A later sweep can reclaim leftover rows. Admission already committed.
            if (!$error instanceof RetryableException) {
                $this->logger?->warning('Rate-limit expiry cleanup failed', ['exception' => $error::class]);
            }
        }
    }
}
