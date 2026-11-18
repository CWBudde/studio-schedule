<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use RuntimeException;
use StudioSchedule\Domain\BrandingImage;
use StudioSchedule\Service\PublicSettingsPersistence;

/** @phpstan-import-type SettingsRow from PublicSettingsPersistence */
final readonly class PublicSettingsStore implements PublicSettingsPersistence
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
}
