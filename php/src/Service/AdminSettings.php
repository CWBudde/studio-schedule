<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeZone;
use SensitiveParameter;
use StudioSchedule\Domain\AccentColor;
use StudioSchedule\Domain\BrandingImage;
use StudioSchedule\Domain\Currency;

/**
 * @phpstan-import-type RegionalRow from AdminSettingsPersistence
 * @phpstan-import-type ReceiptRow from AdminSettingsPersistence
 */
final readonly class AdminSettings
{
    private const int MAX_BACKGROUND_BYTES = 1024 * 1024;
    public const int MAX_BACKGROUND_BASE64 = 1398104;
    public const int MAX_BACKGROUND_BODY = self::MAX_BACKGROUND_BASE64 + 64 * 1024;
    private const int MAX_LOGO_BYTES = 512 * 1024;
    private const int MAX_LOGO_BASE64 = 699052;
    public const array RECEIPT_TEXT_LIMITS = [
        'issuerName' => 200, 'issuerAddress' => 500, 'taxNumber' => 50, 'email' => 254,
        'phone' => 50, 'taxMode' => 14, 'taxNote' => 500, 'footer' => 500, 'receiptPrefix' => 16,
    ];
    private const array TAX_MODES = ['small_business', 'exempt', 'standard'];

    public function __construct(private AdminSettingsPersistence $persistence) {}

    /** @return array<string, string> */
    public function school(): array
    {
        return PublicSettings::schoolFields($this->persistence->settings() ?? throw new SettingsNotFound());
    }

    /** @return array<string, string> */
    public function updateSchool(string $name, string $notice): array
    {
        $trimmedName = SetupInput::trim($name);
        if ($trimmedName === '' || mb_strlen($name, 'UTF-8') > 200 || str_contains($name, "\0")) {
            throw new AdminSettingsFieldError('schoolName', 'school name must contain 1 to 200 characters');
        }
        if (mb_strlen($notice, 'UTF-8') > 2000 || str_contains($notice, "\0")) {
            throw new AdminSettingsFieldError('legalNotice', 'legal notice must contain at most 2000 characters');
        }
        $notice = SetupInput::trim($notice);

        return PublicSettings::schoolFields($this->persistence->writeSchool($trimmedName, $notice === '' ? null : $notice));
    }

    /** @return array<string, string|bool|float> */
    public function branding(): array
    {
        $settings = $this->persistence->settings() ?? throw new SettingsNotFound();

        return [...PublicSettings::brandingFields($settings), 'minContrast' => AccentColor::MIN_CONTRAST];
    }

    /** @return array<string, string|bool|float> */
    public function updateBranding(string $accent, ?string $theme): array
    {
        if ($accent !== '') {
            if (preg_match('/\A#[0-9a-fA-F]{6}\z/', $accent) !== 1) {
                throw new AdminSettingsFieldError('accentColor', 'accent color must be #rrggbb');
            }
            if (!AccentColor::hasMinimumContrast($accent)) {
                throw new AdminSettingsFieldError('accentColor', 'accent color must reach a contrast of 4.5:1 against the text on it');
            }
        }
        if ($theme !== null && !in_array($theme, ['programme', 'werkstatt'], true)) {
            throw new AdminSettingsFieldError('theme', 'theme must be programme or werkstatt');
        }
        $settings = $this->persistence->writeBranding($accent === '' ? null : strtolower($accent), $theme);

        return [...PublicSettings::brandingFields($settings), 'minContrast' => AccentColor::MIN_CONTRAST];
    }

    public function updateBackground(string $type, string $encoded): void
    {
        $data = self::backgroundData($type, $encoded);
        $this->persistence->writeImage(BrandingImage::Background, $type, $data, BrandingImage::digest($type, $data));
    }

    public static function backgroundData(string $type, #[SensitiveParameter] string $encoded): string
    {
        if (!in_array($type, ['image/jpeg', 'image/png', 'image/webp'], true)) {
            throw new AdminSettingsFieldError('contentType', 'the background must be a JPEG, PNG or WebP image');
        }
        if (strlen($encoded) > self::MAX_BACKGROUND_BASE64) {
            throw new AdminSettingsFieldError('data', 'the background must be at most 1 MiB');
        }
        $data = self::decodeImage($encoded);
        if ($data === '' || strlen($data) > self::MAX_BACKGROUND_BYTES) {
            throw new AdminSettingsFieldError('data', $data === '' ? 'the background is empty' : 'the background must be at most 1 MiB');
        }

        if (!self::matchesImageType($type, $data)) {
            throw new AdminSettingsFieldError('data', "the background's data does not match its type");
        }

        return $data;
    }

    public function deleteBackground(): void
    {
        $this->persistence->writeImage(BrandingImage::Background, null, null, null);
    }

    /** @return array<string,string|int|bool> */
    public function receiptSettings(): array
    {
        return self::receiptFields($this->persistence->receiptSettings() ?? throw new SettingsNotFound());
    }

    /** @return array{redemptionMode:string} */
    public function passSettings(): array
    {
        return ['redemptionMode' => $this->persistence->redemptionMode() ?? throw new SettingsNotFound()];
    }

    /** @return array{redemptionMode:string} */
    public function updatePassSettings(string $mode): array
    {
        if (!in_array($mode, ['booking', 'attendance'], true)) {
            throw new AdminSettingsFieldError('redemptionMode', 'invalid redemption mode');
        }
        $this->persistence->writeRedemptionMode($mode);

        return ['redemptionMode' => $mode];
    }

    /**
     * @param array<string,string> $input
     * @return array<string,string|int|bool>
     */
    public function updateReceiptSettings(#[SensitiveParameter] array $input): array
    {
        foreach (self::RECEIPT_TEXT_LIMITS as $name => $limit) {
            $value = $input[$name] ?? '';
            if (mb_strlen($value, 'UTF-8') > $limit || str_contains($value, "\0")) {
                throw new AdminSettingsFieldError($name, "expected text of at most $limit characters");
            }
        }

        $prefix = SetupInput::trim($input['receiptPrefix'] ?? '');
        $tax = SetupInput::trim($input['taxNumber'] ?? '');
        $phone = SetupInput::trim($input['phone'] ?? '');
        $email = SetupInput::trim($input['email'] ?? '');
        $mode = $input['taxMode'] ?? '';
        if (preg_match('/\A[A-Z0-9]{1,16}\z/', $prefix) !== 1) {
            throw new AdminSettingsFieldError('receiptPrefix', 'receipt prefix must be 1-16 of A-Z and 0-9');
        }
        if (str_contains($tax, "\n")) {
            throw new AdminSettingsFieldError('taxNumber', 'the tax number must be one line of at most 50 characters');
        }
        if (str_contains($phone, "\n")) {
            throw new AdminSettingsFieldError('phone', 'the phone number must be one line of at most 50 characters');
        }
        if ($email !== '' && !SetupInput::validEmail($email)) {
            throw new AdminSettingsFieldError('email', 'invalid email address');
        }
        if (isset($input['taxMode']) && !in_array($mode, self::TAX_MODES, true)) {
            throw new AdminSettingsFieldError('taxMode', 'invalid tax mode');
        }

        return self::receiptFields($this->persistence->writeReceiptSettings([
            'receipt_prefix' => $prefix,
            'issuer_name' => self::optionalText(SetupInput::trim($input['issuerName'] ?? '')),
            'issuer_address' => self::optionalText(self::trimLines($input['issuerAddress'] ?? '')),
            'issuer_tax_id' => self::optionalText($tax),
            'issuer_email' => self::optionalText($email),
            'issuer_phone' => self::optionalText($phone),
            'tax_mode' => self::optionalText($mode),
            'tax_note' => self::optionalText(SetupInput::trim($input['taxNote'] ?? '')),
            'receipt_footer' => self::optionalText(self::trimLines($input['footer'] ?? '')),
        ]));
    }

    /** @return array{type:string,data:string}|null */
    public function receiptLogo(): ?array
    {
        $image = $this->persistence->image(BrandingImage::Logo) ?? throw new SettingsNotFound();

        return $image['contentType'] === null || $image['data'] === null
            ? null
            : ['type' => $image['contentType'], 'data' => $image['data']];
    }

    public function updateReceiptLogo(string $type, #[SensitiveParameter] string $encoded): void
    {
        if (!in_array($type, ['image/png', 'image/jpeg'], true)) {
            throw new AdminSettingsFieldError('contentType', 'the logo must be a PNG or JPEG image');
        }
        if (strlen($encoded) > self::MAX_LOGO_BASE64) {
            throw new AdminSettingsFieldError('data', 'the logo must be at most 512 KiB');
        }
        $data = self::decodeImage($encoded);
        if ($data === '') {
            throw new AdminSettingsFieldError('data', 'the logo is empty');
        }
        if (strlen($data) > self::MAX_LOGO_BYTES) {
            throw new AdminSettingsFieldError('data', 'the logo must be at most 512 KiB');
        }
        if (!self::matchesImageType($type, $data)) {
            throw new AdminSettingsFieldError('data', "the logo's data does not match its type");
        }

        $this->persistence->writeImage(BrandingImage::Logo, $type, $data, BrandingImage::digest($type, $data));
    }

    public function deleteReceiptLogo(): void
    {
        $this->persistence->writeImage(BrandingImage::Logo, null, null, null);
    }

    private static function decodeImage(#[SensitiveParameter] string $encoded): string
    {
        // The OpenAPI base64 schema rejects whitespace and Go requires standard padding.
        if (strlen($encoded) % 4 !== 0 || preg_match('/[^A-Za-z0-9+\/=]/', $encoded) === 1 || ($data = base64_decode($encoded, true)) === false) {
            throw new AdminSettingsFieldError('data', 'expected a base64-encoded image');
        }

        return $data;
    }

    private static function matchesImageType(string $type, #[SensitiveParameter] string $data): bool
    {
        return match ($type) {
            'image/jpeg' => str_starts_with($data, "\xff\xd8\xff"),
            'image/png' => str_starts_with($data, "\x89PNG\r\n\x1a\n"),
            'image/webp' => str_starts_with($data, 'RIFF') && substr($data, 8, 4) === 'WEBP',
            default => false,
        };
    }

    private static function trimLines(string $text): string
    {
        $lines = array_map(SetupInput::trim(...), explode("\n", $text));

        return implode("\n", array_filter($lines, static fn(string $line): bool => $line !== ''));
    }

    private static function optionalText(string $value): ?string
    {
        return $value === '' ? null : $value;
    }

    /**
     * @param ReceiptRow $row
     * @return array<string,string|int|bool>
     */
    private static function receiptFields(array $row): array
    {
        $fields = [
            'issuerName' => $row['issuer_name'], 'issuerAddress' => $row['issuer_address'],
            'taxNumber' => $row['issuer_tax_id'], 'email' => $row['issuer_email'], 'phone' => $row['issuer_phone'],
            'taxMode' => $row['tax_mode'], 'taxNote' => $row['tax_note'], 'footer' => $row['receipt_footer'],
            'logoType' => $row['logo_type'],
        ];
        $missing = null;
        foreach (['issuerName', 'issuerAddress', 'taxNumber', 'taxMode'] as $required) {
            if (SetupInput::trim($fields[$required] ?? '') === '') {
                $missing = $required;
                break;
            }
        }
        if ($missing === null && $row['tax_mode'] === 'exempt' && SetupInput::trim($row['tax_note'] ?? '') === '') {
            $missing = 'taxNote';
        }
        $result = array_filter($fields, static fn(?string $value): bool => $value !== null && $value !== '');
        $result['receiptPrefix'] = $row['receipt_prefix'];
        $result['complete'] = $missing === null;
        if ($missing !== null) {
            $result['missing'] = $missing;
        }
        $result['logoMaxBytes'] = self::MAX_LOGO_BYTES;
        if ($row['tax_mode'] === 'standard' && $row['currency'] === 'EUR') {
            $result['addressRequiredAbove'] = 25000;
        }

        return $result;
    }

    /** @return array<string, string|bool> */
    public function regional(): array
    {
        return self::regionalFields($this->persistence->regional() ?? throw new SettingsNotFound());
    }

    /** @return array<string, string|bool> */
    public function updateRegional(string $timezone, string $locale, string $currency): array
    {
        if ($timezone === '' || mb_strlen($timezone, 'UTF-8') > 64) {
            throw new AdminSettingsFieldError('timezone', 'time zone must contain 1 to 64 characters');
        }
        if (!in_array($locale, ['en', 'de'], true)) {
            throw new AdminSettingsFieldError('defaultLocale', 'default locale must be en or de');
        }
        if (preg_match('/\A[A-Z]{3}\z/', $currency) !== 1) {
            throw new AdminSettingsFieldError('currency', 'currency must be three uppercase letters');
        }
        $timezone = SetupInput::trim($timezone);

        $settings = $this->persistence->changeRegional(static function (array $current) use ($timezone, $locale, $currency): array {
            if ($current['timezone_locked'] && $timezone !== $current['timezone']) {
                throw new AdminSettingsConflict('timezone-locked', 'the time zone cannot change once lessons exist');
            }
            if ($current['currency_locked'] && $currency !== $current['currency']) {
                throw new AdminSettingsConflict('currency-locked', 'the currency cannot change once prices or receipts exist');
            }
            if (!in_array($timezone, DateTimeZone::listIdentifiers(DateTimeZone::ALL_WITH_BC), true)) {
                throw new AdminSettingsFieldError('timezone', 'choose an IANA time zone');
            }
            if (Currency::exponent($currency) === null) {
                throw new AdminSettingsFieldError('currency', 'currency must be an ISO 4217 code');
            }

            return ['timezone' => $timezone, 'default_locale' => $locale, 'currency' => $currency];
        });

        return self::regionalFields($settings);
    }

    /**
     * @param RegionalRow $settings
     * @return array<string, string|bool>
     */
    private static function regionalFields(array $settings): array
    {
        return [
            'timezone' => $settings['timezone'],
            'defaultLocale' => PublicSettings::schoolLocale($settings['default_locale']),
            'currency' => $settings['currency'],
            'timezoneLocked' => $settings['timezone_locked'],
            'currencyLocked' => $settings['currency_locked'],
        ];
    }
}
