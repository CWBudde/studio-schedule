<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

final class AccentColor
{
    private const float DARK_LIGHTNESS = 0.78;
    private const float DARK_MAX_CHROMA = 0.1;

    /** Match the OKLCh conversion in internal/domain/color.go. */
    public static function dark(string $hex): ?string
    {
        if (preg_match('/\A#[0-9a-fA-F]{6}\z/', $hex) !== 1) {
            return null;
        }

        $r = self::linear((int) hexdec(substr($hex, 1, 2)));
        $g = self::linear((int) hexdec(substr($hex, 3, 2)));
        $b = self::linear((int) hexdec(substr($hex, 5, 2)));
        $l = (0.4122214708 * $r + 0.5363325363 * $g + 0.0514459929 * $b) ** (1 / 3);
        $m = (0.2119034982 * $r + 0.6806995451 * $g + 0.1073969566 * $b) ** (1 / 3);
        $s = (0.0883024619 * $r + 0.2817188376 * $g + 0.6299787005 * $b) ** (1 / 3);
        $labA = 1.9779984951 * $l - 2.4285922050 * $m + 0.4505937099 * $s;
        $labB = 0.0259040371 * $l + 0.7827717662 * $m - 0.8086757660 * $s;
        $hue = atan2($labB, $labA) * 180 / M_PI;
        if ($hue < 0) {
            $hue += 360;
        }

        // Reduce chroma until the lighter variant fits into sRGB.
        for ($chroma = min(hypot($labA, $labB), self::DARK_MAX_CHROMA); $chroma > 0; $chroma -= 0.002) {
            $dark = self::fromOKLCh($chroma, $hue);
            if ($dark !== null) {
                return $dark;
            }
        }

        return self::fromOKLCh(0, 0);
    }

    private static function fromOKLCh(float $chroma, float $hue): ?string
    {
        $a = $chroma * cos($hue * M_PI / 180);
        $b = $chroma * sin($hue * M_PI / 180);
        $l = self::DARK_LIGHTNESS + 0.3963377774 * $a + 0.2158037573 * $b;
        $m = self::DARK_LIGHTNESS - 0.1055613458 * $a - 0.0638541728 * $b;
        $s = self::DARK_LIGHTNESS - 0.0894841775 * $a - 1.2914855480 * $b;
        $l = $l * $l * $l;
        $m = $m * $m * $m;
        $s = $s * $s * $s;

        $r = 4.0767416621 * $l - 3.3077115913 * $m + 0.2309699292 * $s;
        $g = -1.2684380046 * $l + 2.6097574011 * $m - 0.3413193965 * $s;
        $b = -0.0041960863 * $l - 0.7034186147 * $m + 1.7076147010 * $s;
        if (min($r, $g, $b) < -1e-4 || max($r, $g, $b) > 1 + 1e-4) {
            return null;
        }

        return sprintf('#%02x%02x%02x', self::gamma($r), self::gamma($g), self::gamma($b));
    }

    private static function linear(int $v): float
    {
        $c = $v / 255;

        return $c <= 0.04045 ? $c / 12.92 : (($c + 0.055) / 1.055) ** 2.4;
    }

    private static function gamma(float $v): int
    {
        $v = min(max($v, 0), 1);
        $v = $v <= 0.0031308 ? $v * 12.92 : 1.055 * $v ** (1 / 2.4) - 0.055;

        return (int) round($v * 255);
    }
}
