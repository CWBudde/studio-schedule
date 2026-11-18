<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\DriverManager;
use Doctrine\DBAL\Tools\DsnParser;
use InvalidArgumentException;
use PDO;

final class Database
{
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
}
