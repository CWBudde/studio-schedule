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

    public static function connect(#[\SensitiveParameter] string $url, bool $dedicated = false): Connection
    {
        $scheme = parse_url($url, PHP_URL_SCHEME);
        if (!is_string($scheme) || !isset(self::DRIVERS[strtolower($scheme)])) {
            throw new InvalidArgumentException('DATABASE_URL must name PostgreSQL, MySQL or MariaDB');
        }

        $driver = self::DRIVERS[strtolower($scheme)];
        $params = (new DsnParser(self::DRIVERS))->parse($url);
        unset($params['driverClass']);
        $params['driver'] = $driver;
        $params['charset'] = $driver === 'pdo_mysql' ? 'utf8mb4' : 'UTF8';

        if ($dedicated) {
            $params['persistent'] = false;
            $params['driverOptions'][PDO::ATTR_PERSISTENT] = false;
        }

        return DriverManager::getConnection($params);
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
