<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;

/**
 * @phpstan-import-type SettingsRow from PublicSettingsPersistence
 * @phpstan-type RegionalValues array{timezone: string, default_locale: string, currency: string}
 * @phpstan-type RegionalRow array{timezone: string, default_locale: string, currency: string, timezone_locked: bool, currency_locked: bool}
 */
interface AdminSettingsPersistence extends PublicSettingsPersistence
{
    /** @return SettingsRow */
    public function writeSchool(string $name, ?string $notice): array;

    /** @return SettingsRow */
    public function writeBranding(?string $accent, ?string $theme): array;

    public function writeBackground(?string $type, ?string $data, ?string $digest): void;

    /** @return RegionalRow|null */
    public function regional(): ?array;

    /**
     * @param Closure(RegionalRow):RegionalValues $change
     * @return RegionalRow
     */
    public function changeRegional(Closure $change): array;
}
