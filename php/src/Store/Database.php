<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\DriverManager;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use Doctrine\DBAL\Tools\DsnParser;
use InvalidArgumentException;
use PDO;

final class Database
{
    private const string READ_SNAPSHOT = 'SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY';

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
}
