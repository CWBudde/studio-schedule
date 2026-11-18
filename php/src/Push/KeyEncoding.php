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

    public static function decode(#[SensitiveParameter] string $value, int $length): string
    {
        $value = rtrim(trim($value), '=');
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
