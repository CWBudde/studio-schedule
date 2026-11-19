<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use StudioSchedule\Domain\BrandingImage;

/**
 * @phpstan-import-type SettingsRow from PublicSettingsPersistence
 * @phpstan-type ReceiptValues array{
 *     receipt_prefix:string, issuer_name:?string, issuer_address:?string, issuer_tax_id:?string,
 *     issuer_email:?string, issuer_phone:?string, tax_mode:?string, tax_note:?string, receipt_footer:?string
 * }
 * @phpstan-type ReceiptRow array{
 *     receipt_prefix:string, issuer_name:?string, issuer_address:?string, issuer_tax_id:?string,
 *     issuer_email:?string, issuer_phone:?string, tax_mode:?string, tax_note:?string, receipt_footer:?string,
 *     currency:string, logo_type:?string
 * }
 * @phpstan-type RegionalValues array{timezone: string, default_locale: string, currency: string}
 * @phpstan-type RegionalRow array{timezone: string, default_locale: string, currency: string, timezone_locked: bool, currency_locked: bool}
 */
interface AdminSettingsPersistence extends PublicSettingsPersistence
{
    /** @return SettingsRow */
    public function writeSchool(string $name, ?string $notice): array;

    /** @return SettingsRow */
    public function writeBranding(?string $accent, ?string $theme): array;

    public function writeImage(BrandingImage $image, ?string $type, ?string $data, ?string $digest): void;

    /** @return ReceiptRow|null */
    public function receiptSettings(): ?array;

    /**
     * @param ReceiptValues $values
     * @return ReceiptRow
     */
    public function writeReceiptSettings(array $values): array;

    /** @return RegionalRow|null */
    public function regional(): ?array;

    /**
     * @param Closure(RegionalRow):RegionalValues $change
     * @return RegionalRow
     */
    public function changeRegional(Closure $change): array;
}
