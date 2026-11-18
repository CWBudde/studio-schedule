<?php

declare(strict_types=1);

namespace StudioSchedule\Push;

use Base64Url\Base64Url;
use InvalidArgumentException;
use SensitiveParameter;

/** @internal */
final class KeyEncoding
{
    public const CURVE = 'prime256v1';

    public static function browserPublicKey(#[SensitiveParameter] string $value): string
    {
        $point = self::decode($value, 65);
        if ($point[0] !== "\x04" || openssl_pkey_new(['ec' => [
            'curve_name' => self::CURVE,
            'x' => substr($point, 1, 32),
            'y' => substr($point, 33, 32),
        ]]) === false) {
            throw new InvalidArgumentException('webpush: browser key is not an uncompressed P-256 point');
        }

        return $point;
    }

    public static function decode(#[SensitiveParameter] string $value, int $length): string
    {
        $value = preg_replace('/\A\s+|\s+\z/u', '', $value);
        if ($value === null) {
            throw new InvalidArgumentException('webpush: key is not base64url');
        }
        // Go's base64url decoder ignores these line endings and accepts padding.
        $value = str_replace(["\r", "\n"], '', rtrim($value, '='));
        if (preg_match('/\A[A-Za-z0-9_-]+\z/', $value) !== 1) {
            throw new InvalidArgumentException('webpush: key is not base64url');
        }
        $raw = Base64Url::decode($value);
        if (strlen($raw) !== $length) {
            throw new InvalidArgumentException('webpush: invalid key length');
        }

        return $raw;
    }
}
