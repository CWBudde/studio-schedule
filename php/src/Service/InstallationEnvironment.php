<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

final class InstallationEnvironment
{
    // Runtime requirements of the app and the production dependencies in composer.lock.
    private const array EXTENSIONS = ['curl', 'gd', 'iconv', 'json', 'mbstring', 'openssl', 'pdo', 'zlib'];

    public static function phpProblem(string $databaseDriver): ?string
    {
        $missing = array_filter([...self::EXTENSIONS, $databaseDriver], static fn(string $extension): bool => !extension_loaded($extension));
        if ($missing !== []) {
            return 'Enable the required PHP extensions: ' . implode(', ', $missing) . '.';
        }

        if (!PasswordHash::isAvailable()) {
            return 'Enable Argon2id password hashing and verification in PHP or install the sodium extension.';
        }

        $log = ini_get('error_log');
        if ($log === false || $log === '' || in_array($log, ['syslog', 'php://stderr', '/dev/stderr', '/proc/self/fd/2'], true)) {
            return null;
        }
        // Shared hosts can warn when a configured log path falls outside open_basedir.
        if (!@is_dir(dirname($log)) || !@is_writable(dirname($log))) {
            return "PHP's configured error_log directory must be writable.";
        }
        if (@is_dir($log) || ((@file_exists($log) || @is_link($log)) && !@is_writable($log))) {
            return "PHP's configured error_log must name a writable file.";
        }

        return null;
    }

    public static function databaseProblem(string $driver, string $serverVersion): ?string
    {
        if ($driver === 'pdo_pgsql') {
            // The calendar feeds migration uses NULLS NOT DISTINCT on a unique index.

            return ctype_digit($serverVersion) && (int) $serverVersion >= 150000 ? null : 'Use PostgreSQL 15 or later.';
        }

        if (stripos($serverVersion, 'MariaDB') !== false) {
            $product = 'MariaDB';
            $minimum = '10.6';
            $serverVersion = preg_replace('/^(?:5\.5\.5-)?(?:mariadb-)?/i', '', $serverVersion) ?? '';
        } else {
            $product = 'MySQL';
            $minimum = '8.0.19';
        }

        if (preg_match('/^\d+(?:\.\d+){0,2}/', $serverVersion, $parts) !== 1 || version_compare($parts[0], $minimum, '<')) {
            return "Use $product $minimum or later.";
        }

        return null;
    }
}
