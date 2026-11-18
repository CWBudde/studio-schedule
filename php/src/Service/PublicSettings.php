<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use StudioSchedule\Domain\AccentColor;
use StudioSchedule\Domain\BrandingImage;

final readonly class PublicSettings
{
    public function __construct(private PublicSettingsPersistence $persistence) {}

    /** @return array<string, string|bool> */
    public function settings(): array
    {
        $settings = $this->persistence->settings() ?? throw new SettingsNotFound();
        $locale = strtolower(str_replace('_', '-', SetupInput::trim($settings['default_locale'])));
        $language = explode('-', $locale, 2)[0];
        $theme = $settings['theme'] ?? '';
        $notice = $settings['legal_notice'] ?? '';
        $accent = $settings['accent_color'] ?? '';

        $public = [
            'schoolName' => $settings['school_name'],
            'theme' => $theme === '' ? 'programme' : $theme,
            'hasLogo' => ($settings['logo_digest'] ?? '') !== '',
            'hasBackground' => ($settings['background_digest'] ?? '') !== '',
            'timezone' => $settings['timezone'],
            'defaultLocale' => $language === 'de' ? 'de' : 'en',
            'currency' => $settings['currency'],
        ];
        if ($notice !== '') {
            $public['legalNotice'] = $notice;
        }
        if ($accent !== '') {
            $public['accentColor'] = $accent;
            $dark = AccentColor::dark($accent);
            if ($dark !== null) {
                $public['accentColorDark'] = $dark;
            }
        }

        return $public;
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

        return ['digest' => hash('sha256', $contentType . "\n" . $data), 'contentType' => $contentType, 'data' => $data];
    }
}
