<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

/** @phpstan-import-type SettingsRow from PublicSettingsPersistence */
interface AdminSettingsPersistence extends PublicSettingsPersistence
{
    /** @return SettingsRow */
    public function writeSchool(string $name, ?string $notice): array;

    /** @return SettingsRow */
    public function writeBranding(?string $accent, ?string $theme): array;

    public function writeBackground(?string $type, ?string $data, ?string $digest): void;
}
