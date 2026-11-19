<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use RuntimeException;
use StudioSchedule\Domain\BrandingImage;
use StudioSchedule\Service\AdminSettingsPersistence;
use StudioSchedule\Service\PublicSettingsPersistence;
use StudioSchedule\Service\SettingsNotFound;

/** @phpstan-import-type SettingsRow from PublicSettingsPersistence */
final readonly class PublicSettingsStore implements AdminSettingsPersistence
{
    public function __construct(private Connection $connection) {}

    public function settings(): ?array
    {
        /** @var SettingsRow|false $row */
        $row = $this->connection->fetchAssociative(
            'SELECT school_name, timezone, default_locale, currency, legal_notice, accent_color, theme, '
            . 'logo_digest, background_digest FROM settings',
        );

        return $row === false ? null : $row;
    }

    public function imageInfo(BrandingImage $image): ?array
    {
        $name = $image->value;
        /** @var array{digest: ?string}|false $row */
        $row = $this->connection->fetchAssociative("SELECT {$name}_digest AS digest FROM settings");

        return $row === false ? null : $row;
    }

    public function image(BrandingImage $image): ?array
    {
        $name = $image->value;
        /** @var array{?string, string|resource|null}|false $row */
        $row = $this->connection->fetchNumeric("SELECT {$name}_type, {$name} FROM settings");
        if ($row === false) {
            return null;
        }

        [$contentType, $data] = $row;
        // PDO PostgreSQL can return bytea as a stream. MySQL returns a string.
        if ($data !== null && !is_string($data)) {
            $stream = $data;
            $data = stream_get_contents($stream);
            fclose($stream);
            if ($data === false) {
                throw new RuntimeException('could not read branding image');
            }
        }

        return ['contentType' => $contentType, 'data' => $data];
    }

    public function writeSchool(string $name, ?string $notice): array
    {
        return Database::retryWrite($this->connection, function () use ($name, $notice): array {
            $this->connection->executeStatement('UPDATE settings SET school_name = ?, legal_notice = ?', [$name, $notice]);

            return $this->settings() ?? throw new SettingsNotFound();
        });
    }

    public function writeBranding(?string $accent, ?string $theme): array
    {
        return Database::retryWrite($this->connection, function () use ($accent, $theme): array {
            $this->connection->executeStatement('UPDATE settings SET accent_color = ?, theme = COALESCE(?, theme)', [$accent, $theme]);

            return $this->settings() ?? throw new SettingsNotFound();
        });
    }

    public function writeBackground(?string $type, ?string $data, ?string $digest): void
    {
        Database::retryWrite($this->connection, function () use ($type, $data, $digest): void {
            $this->lockSettings();
            $this->connection->executeStatement(
                'UPDATE settings SET background = ?, background_type = ?, background_digest = ?',
                [$data, $type, $digest],
                [$data === null ? ParameterType::NULL : ParameterType::LARGE_OBJECT, ParameterType::STRING, ParameterType::STRING],
            );
        });
    }

    public function regional(): ?array
    {
        /** @var array{timezone: string, default_locale: string, currency: string, timezone_locked: bool|int|string, currency_locked: bool|int|string}|false $row */
        $row = $this->connection->fetchAssociative(
            'SELECT timezone, default_locale, currency, EXISTS (SELECT 1 FROM lessons) AS timezone_locked, ('
            . 'EXISTS (SELECT 1 FROM pass_types WHERE price > 0) OR EXISTS (SELECT 1 FROM passes WHERE price_paid > 0) '
            . 'OR EXISTS (SELECT 1 FROM events WHERE price > 0) OR EXISTS (SELECT 1 FROM event_registrations WHERE price > 0) '
            . 'OR EXISTS (SELECT 1 FROM receipts)) AS currency_locked FROM settings',
        );
        if ($row === false) {
            return null;
        }
        $row['timezone_locked'] = in_array($row['timezone_locked'], [true, 1, '1', 't'], true);
        $row['currency_locked'] = in_array($row['currency_locked'], [true, 1, '1', 't'], true);

        return $row;
    }

    public function changeRegional(Closure $change): array
    {
        return Database::retryWrite($this->connection, function () use ($change): array {
            $this->lockSettings();
            $current = $this->regional() ?? throw new SettingsNotFound();
            $updated = $change($current);
            $this->connection->executeStatement(
                'UPDATE settings SET timezone = ?, default_locale = ?, currency = ?',
                [$updated['timezone'], $updated['default_locale'], $updated['currency']],
            );

            return $updated + $current;
        });
    }

    private function lockSettings(): void
    {
        if ($this->connection->fetchOne('SELECT 1 FROM settings FOR UPDATE') === false) {
            throw new SettingsNotFound();
        }
    }
}
