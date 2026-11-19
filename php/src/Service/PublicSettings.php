<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use StudioSchedule\Domain\AccentColor;
use StudioSchedule\Domain\BrandingImage;

/** @phpstan-import-type SettingsRow from PublicSettingsPersistence */
final readonly class PublicSettings
{
    public function __construct(private PublicSettingsPersistence $persistence) {}

    /** @return array<string, string|bool> */
    public function settings(): array
    {
        $settings = $this->persistence->settings() ?? throw new SettingsNotFound();

        return [
            ...self::schoolFields($settings),
            ...self::brandingFields($settings),
            'timezone' => $settings['timezone'],
            'defaultLocale' => self::schoolLocale($settings['default_locale']),
            'currency' => $settings['currency'],
        ];
    }

    public static function schoolLocale(string $locale): string
    {
        $locale = strtolower(str_replace('_', '-', SetupInput::trim($locale)));

        return explode('-', $locale, 2)[0] === 'de' ? 'de' : 'en';
    }

    /**
     * @param SettingsRow $settings
     * @return array<string, string>
     */
    public static function schoolFields(array $settings): array
    {
        $school = ['schoolName' => $settings['school_name']];
        $notice = $settings['legal_notice'] ?? '';
        if ($notice !== '') {
            $school['legalNotice'] = $notice;
        }

        return $school;
    }

    /**
     * @param SettingsRow $settings
     * @return array<string, string|bool>
     */
    public static function brandingFields(array $settings): array
    {
        $theme = $settings['theme'] ?? '';
        $accent = $settings['accent_color'] ?? '';
        $branding = [
            'theme' => $theme === '' ? 'programme' : $theme,
            'hasLogo' => ($settings['logo_digest'] ?? '') !== '',
            'hasBackground' => ($settings['background_digest'] ?? '') !== '',
        ];
        if ($accent !== '') {
            $branding['accentColor'] = $accent;
            $dark = AccentColor::dark($accent);
            if ($dark !== null) {
                $branding['accentColorDark'] = $dark;
            }
        }

        return $branding;
    }

    /**
     * @param callable(string):bool $matches
     * @return array{digest: string, contentType: string, data: string}|array{digest: string}|null
     */
    public function image(BrandingImage $image, callable $matches): ?array
    {
        $info = $this->persistence->imageInfo($image) ?? throw new SettingsNotFound();
        $digest = $info['digest'];
        if ($digest === null || $digest === '') {
            return null;
        }
        if ($matches($digest)) {
            return ['digest' => $digest];
        }

        // The image may have changed after reading the stored digest.
        // Compute the validator from the content type and bytes of this read.
        $asset = $this->persistence->image($image) ?? throw new SettingsNotFound();
        $data = $asset['data'];
        if ($data === null || $data === '') {
            return null;
        }

        $contentType = $asset['contentType'] ?? '';

        return ['digest' => BrandingImage::digest($contentType, $data), 'contentType' => $contentType, 'data' => $data];
    }
}
