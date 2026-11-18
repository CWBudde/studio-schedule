<?php

declare(strict_types=1);

namespace StudioSchedule\Console;

use InvalidArgumentException;
use StudioSchedule\Store\MigrationError;
use StudioSchedule\Store\Migrator;
use UnexpectedValueException;

final class MigrationCommand
{
    /** @param resource $output */
    public static function run(
        string $direction,
        #[\SensitiveParameter]
        string $databaseURL,
        string $postgresMigrations,
        string $mysqlMigrations,
        bool $backupVerified,
        $output,
    ): void {
        if ($databaseURL === '') {
            throw new CommandError('no database URL: set DATABASE_URL or --database-url', 2);
        }
        $scheme = strtolower((string) parse_url($databaseURL, PHP_URL_SCHEME));
        $directory = match ($scheme) {
            'postgres', 'postgresql' => $postgresMigrations,
            'mysql', 'mariadb' => $mysqlMigrations,
            default => throw new CommandError('DATABASE_URL must name PostgreSQL, MySQL or MariaDB', 2),
        };
        $migrator = new Migrator($databaseURL);

        try {
            if ($direction === 'status') {
                self::printStatus($output, $migrator->status($directory));

                return;
            }
            $results = $direction === 'up'
                ? $migrator->upWithResults($directory, $backupVerified)
                : [$migrator->down($directory, $backupVerified)];
        } catch (MigrationError $error) {
            $suffix = $error->needsBackup ? ', then pass --backup-verified' : '';
            throw new CommandError($error->getMessage() . $suffix);
        } catch (UnexpectedValueException $error) {
            throw new CommandError($error->getMessage());
        } catch (InvalidArgumentException) {
            throw new CommandError('Invalid DATABASE_URL', 2);
        }

        if ($results === []) {
            fwrite($output, "schema is up to date\n");
        }
        foreach ($results as $result) {
            fwrite($output, sprintf("%-4s %s (%s)\n", $direction, $result['name'], self::duration($result['microseconds'])));
        }
    }

    /**
     * @param resource $output
     * @param list<array{version:int,name:string,appliedAt:?int}> $migrations
     */
    private static function printStatus($output, array $migrations): void
    {
        $rows = [['VERSION', 'STATE', 'APPLIED AT', 'NAME']];
        $widths = [7, 5, 10];
        foreach ($migrations as $migration) {
            $applied = $migration['appliedAt'];
            $row = [
                (string) $migration['version'],
                $applied === null ? 'pending' : 'applied',
                $applied === null ? '-' : gmdate('Y-m-d\TH:i:s\Z', $applied),
                $migration['name'],
            ];
            foreach ($widths as $index => $width) {
                $widths[$index] = max($width, strlen($row[$index]));
            }
            $rows[] = $row;
        }

        foreach ($rows as [$version, $state, $appliedAt, $name]) {
            fwrite(
                $output,
                str_pad($version, $widths[0] + 2) . str_pad($state, $widths[1] + 2) . str_pad($appliedAt, $widths[2] + 2) . $name . "\n",
            );
        }
    }

    /** Render a Go duration rounded to microseconds. */
    private static function duration(int $microseconds): string
    {
        if ($microseconds === 0) {
            return '0s';
        }
        if ($microseconds < 1000) {
            return $microseconds . 'µs';
        }
        if ($microseconds < 1000000) {
            return rtrim(rtrim(sprintf('%.3F', $microseconds / 1000), '0'), '.') . 'ms';
        }

        $seconds = intdiv($microseconds, 1000000);
        $hours = intdiv($seconds, 3600);
        $minutes = intdiv($seconds % 3600, 60);
        $fraction = $microseconds % 1000000;
        $secondPart = (string) ($seconds % 60) . ($fraction === 0 ? '' : '.' . rtrim(sprintf('%06d', $fraction), '0')) . 's';

        return ($hours === 0 ? '' : $hours . 'h') . ($minutes === 0 && $hours === 0 ? '' : $minutes . 'm') . $secondPart;
    }
}
