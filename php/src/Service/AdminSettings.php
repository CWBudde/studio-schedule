<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use StudioSchedule\Domain\AccentColor;
use StudioSchedule\Domain\BrandingImage;

final readonly class AdminSettings
{
    private const int MAX_BACKGROUND_BYTES = 1024 * 1024;
    private const int MAX_BACKGROUND_BASE64 = 1398104;
    public const int MAX_BACKGROUND_BODY = self::MAX_BACKGROUND_BASE64 + 64 * 1024;

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
        if (!in_array($type, ['image/jpeg', 'image/png', 'image/webp'], true)) {
            throw new AdminSettingsFieldError('contentType', 'the background must be a JPEG, PNG or WebP image');
        }
        if (strlen($encoded) > self::MAX_BACKGROUND_BASE64) {
            throw new AdminSettingsFieldError('data', 'the background must be at most 1 MiB');
        }
        // The OpenAPI base64 schema rejects whitespace and Go requires standard padding.
        if (strlen($encoded) % 4 !== 0 || preg_match('/[^A-Za-z0-9+\/=]/', $encoded) === 1 || ($data = base64_decode($encoded, true)) === false) {
            throw new AdminSettingsFieldError('data', 'expected a base64-encoded image');
        }
        if ($data === '' || strlen($data) > self::MAX_BACKGROUND_BYTES) {
            throw new AdminSettingsFieldError('data', $data === '' ? 'the background is empty' : 'the background must be at most 1 MiB');
        }

        $matches = match ($type) {
            'image/jpeg' => str_starts_with($data, "\xff\xd8\xff"),
            'image/png' => str_starts_with($data, "\x89PNG\r\n\x1a\n"),
            'image/webp' => str_starts_with($data, 'RIFF') && substr($data, 8, 4) === 'WEBP',
        };
        if (!$matches) {
            throw new AdminSettingsFieldError('data', "the background's data does not match its type");
        }

        $this->persistence->writeBackground($type, $data, BrandingImage::digest($type, $data));
    }

    public function deleteBackground(): void
    {
        $this->persistence->writeBackground(null, null, null);
    }
}
