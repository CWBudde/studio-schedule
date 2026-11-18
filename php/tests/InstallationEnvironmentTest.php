<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Service\InstallationEnvironment;

final class InstallationEnvironmentTest extends TestCase
{
    #[DataProvider('databaseVersions')]
    public function testMinimumVersionMatchesTheReleaseMigrations(string $driver, string $version, ?string $problem): void
    {
        self::assertSame($problem, InstallationEnvironment::databaseProblem($driver, $version));
    }

    /** @return iterable<string,array{string,string,?string}> */
    public static function databaseVersions(): iterable
    {
        yield 'PostgreSQL with nulls-not-distinct indexes' => ['pdo_pgsql', '150000', null];
        yield 'older PostgreSQL' => ['pdo_pgsql', '140022', 'Use PostgreSQL 15 or later.'];
        yield 'commercial MySQL build' => ['pdo_mysql', '8.0.19-commercial', null];
        yield 'older MySQL' => ['pdo_mysql', '8.0.18', 'Use MySQL 8.0.19 or later.'];
        yield 'MariaDB compatibility prefix' => ['pdo_mysql', '5.5.5-10.6.24-MariaDB', null];
        yield 'MariaDB distribution package' => ['pdo_mysql', '10.11.14-MariaDB-0+deb12u2', null];
        yield 'older MariaDB' => ['pdo_mysql', '10.5.29-MariaDB', 'Use MariaDB 10.6 or later.'];
    }
}
