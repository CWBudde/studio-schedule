<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use StudioSchedule\Domain\BrandingImage;

/**
 * @phpstan-type SettingsRow array{
 *     school_name: string, timezone: string, default_locale: string, currency: string,
 *     legal_notice: ?string, accent_color: ?string, theme: ?string, logo_digest: ?string, background_digest: ?string
 * }
 */
interface PublicSettingsPersistence
{
    /** @return SettingsRow|null */
    public function settings(): ?array;

    /** @return array{digest: ?string}|null */
    public function imageInfo(BrandingImage $image): ?array;

    /** @return array{contentType: ?string, data: ?string}|null */
    public function image(BrandingImage $image): ?array;
}
