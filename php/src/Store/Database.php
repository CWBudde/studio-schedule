<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\DriverManager;
use Doctrine\DBAL\Exception\RetryableException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use Doctrine\DBAL\Tools\DsnParser;
use InvalidArgumentException;
use PDO;
use Throwable;

final class Database
{
    private const string READ_SNAPSHOT = 'SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY';
    private const int MAX_WRITE_ATTEMPTS = 3;

    private const DRIVERS = [
        'postgres' => 'pdo_pgsql',
        'postgresql' => 'pdo_pgsql',
        'mysql' => 'pdo_mysql',
        'mariadb' => 'pdo_mysql',
    ];

    public static function connect(#[\SensitiveParameter] string $url, bool $dedicated = false, ?int $timeoutSeconds = null): Connection
    {
        $driver = self::driverForURL($url);
        if ($driver === null) {
            throw new InvalidArgumentException('DATABASE_URL must name PostgreSQL, MySQL or MariaDB');
        }

        $params = (new DsnParser(self::DRIVERS))->parse($url);
        unset($params['driverClass']);
        $params['driver'] = $driver;
        $params['charset'] = $driver === 'pdo_mysql' ? 'utf8mb4' : 'UTF8';

        if ($dedicated) {
            $params['persistent'] = false;
            $params['driverOptions'][PDO::ATTR_PERSISTENT] = false;
        }
        if ($timeoutSeconds !== null) {
            $params['driverOptions'][PDO::ATTR_TIMEOUT] = $timeoutSeconds;
        }

        return DriverManager::getConnection($params);
    }

    /** @return 'pdo_pgsql'|'pdo_mysql'|null */
    public static function driverForURL(#[\SensitiveParameter] string $url): ?string
    {
        $scheme = parse_url($url, PHP_URL_SCHEME);

        return is_string($scheme) ? (self::DRIVERS[strtolower($scheme)] ?? null) : null;
    }

    /** @param 'pdo_pgsql'|'pdo_mysql' $driver */
    public static function serverVersion(Connection $connection, string $driver): string
    {
        $version = $connection->fetchOne($driver === 'pdo_pgsql' ? 'SHOW server_version_num' : 'SELECT VERSION()');

        return is_string($version) ? $version : '';
    }

    public static function sqlInstant(Connection $connection, DateTimeImmutable $instant): string
    {
        $utc = $instant->setTimezone(new DateTimeZone('UTC'));
        if (!$connection->getDatabasePlatform() instanceof PostgreSQLPlatform) {
            return $utc->format('Y-m-d H:i:s.u');
        }

        // School midnight on 0001-01-01 may still be in year zero in UTC.
        $text = $utc->format('Y-m-d H:i:s.uP');

        return str_starts_with($text, '0000-') ? '0001' . substr($text, 4) . ' BC' : $text;
    }

    public static function instant(string $text): DateTimeImmutable
    {
        // PostgreSQL writes year zero as year one BC. MySQL timestamps are stored in UTC.
        if (str_starts_with($text, '0001-') && str_ends_with($text, ' BC')) {
            $text = '0000' . substr($text, 4, -3);
        }
        $utc = new DateTimeZone('UTC');

        return (new DateTimeImmutable($text, $utc))->setTimezone($utc);
    }

    /** Convert a PostgreSQL interval using Go's 30-day months. */
    public static function intervalMicros(string $column): string
    {
        $years = "EXTRACT(YEAR FROM $column)";
        $remainder = "$column - make_interval(years => CAST($years AS integer))";
        $secondsPerYear = 360 * 86400;

        return "CAST((EXTRACT(EPOCH FROM ($remainder)) + $years * $secondsPerYear) * 1000000 AS BIGINT)";
    }

    /** @return array{string,string} Inclusive microsecond bounds for a half-open interval. */
    public static function sqlRange(Connection $connection, DateTimeImmutable $start, DateTimeImmutable $end): array
    {
        $utc = new DateTimeZone('UTC');
        $start = $start->setTimezone($utc);
        $last = $end->setTimezone($utc)->modify('-1 microsecond');
        if (!$connection->getDatabasePlatform() instanceof PostgreSQLPlatform) {
            // School midnight can lie outside DATETIME(6)'s calendar in UTC.
            $start = max($start, new DateTimeImmutable('0001-01-01T00:00:00Z'));
            $last = min($last, new DateTimeImmutable('9999-12-31T23:59:59.999999Z'));
        }

        return [self::sqlInstant($connection, $start), self::sqlInstant($connection, $last)];
    }

    /** The lock shared by PHP migrations and copies and by Go Goose on PostgreSQL. */
    public static function lockMigrations(Connection $connection): bool
    {
        $lock = $connection->getDatabasePlatform() instanceof PostgreSQLPlatform
            ? 'SELECT pg_try_advisory_lock(4097083626)'
            : "SELECT GET_LOCK(SHA2(CONCAT('studio-schedule:', DATABASE(), ':migrations'), 256), 0)";

        return in_array($connection->fetchOne($lock), [true, 't', 1, '1'], true);
    }

    /**
     * @template T
     * @param Closure():T $read
     * @return T
     */
    public static function inReadSnapshot(Connection $connection, Closure $read): mixed
    {
        $postgres = $connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
        // MySQL and MariaDB apply SET TRANSACTION to the next transaction.
        if (!$postgres) {
            $connection->executeStatement(self::READ_SNAPSHOT);
        }

        return $connection->transactional(static function (Connection $connection) use ($postgres, $read): mixed {
            if ($postgres) {
                $connection->executeStatement(self::READ_SNAPSHOT);
                // PDO reads date and timestamp text in the server's DateStyle.
                $connection->executeStatement("SET LOCAL DateStyle TO 'ISO, YMD'");
            }

            return $read();
        });
    }

    /**
     * @template T
     * @param Closure():T $write
     * @return T
     */
    public static function retryWrite(Connection $connection, Closure $write): mixed
    {
        for ($attempt = 1; ; ++$attempt) {
            try {
                $connection->beginTransaction();
                $result = $write();
                $connection->commit();

                return $result;
            } catch (Throwable $error) {
                self::abandonTransaction($connection);
                if (!$error instanceof RetryableException || $attempt >= self::MAX_WRITE_ATTEMPTS) {
                    throw $error;
                }
            }
        }
    }

    private static function abandonTransaction(Connection $connection): void
    {
        if ($connection->isTransactionActive()) {
            try {
                $connection->rollBack();

                return;
            } catch (Throwable) {
                // InnoDB may already have rolled back its deadlock victim.
                // Keep the original retryable error and reset the connection.
            }
        }
        $connection->close();
    }
}
