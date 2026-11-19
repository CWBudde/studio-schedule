<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

final class PasswordHash
{
    // Match internal/auth/password/params.go so both backends can use each other's hashes.
    private const int MEMORY_KIB = 19456;
    private const int PASSES = 2;
    private const int LANES = 1;
    private const int SALT_BYTES = 16;
    private const int KEY_BYTES = 32;

    public static function isAvailable(): bool
    {
        $native = self::nativeArgon();
        $hash = ($native && function_exists('password_hash')) || self::sodiumCanHash();
        $verify = ($native && function_exists('password_verify')) || function_exists('sodium_crypto_pwhash_str_verify');

        return $hash && $verify;
    }

    public function hash(#[\SensitiveParameter] string $password): string
    {
        if (function_exists('password_hash') && self::nativeArgon()) {
            return password_hash($password, 'argon2id', [
                'memory_cost' => self::MEMORY_KIB,
                'time_cost' => self::PASSES,
                'threads' => self::LANES,
            ]);
        }

        if (!self::sodiumCanHash()) {
            throw new \RuntimeException('Argon2id password hashing is unavailable');
        }

        $salt = random_bytes(self::SALT_BYTES);
        // PHP's sodium wrapper warns on an empty password while still computing its hash.
        $key = @sodium_crypto_pwhash(
            self::KEY_BYTES,
            $password,
            $salt,
            self::PASSES,
            self::MEMORY_KIB * 1024,
            SODIUM_CRYPTO_PWHASH_ALG_ARGON2ID13,
        );

        return sprintf(
            '$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s',
            self::MEMORY_KIB,
            self::PASSES,
            self::LANES,
            rtrim(base64_encode($salt), '='),
            rtrim(base64_encode($key), '='),
        );
    }

    public function verify(string $encoded, #[\SensitiveParameter] string $password): bool
    {
        if (!self::isBoundedHash($encoded)) {
            return false;
        }

        if (function_exists('password_verify') && self::nativeArgon()) {
            return password_verify($password, $encoded);
        }

        // The wrapper emits the same warning during verification and still returns the comparison.
        return function_exists('sodium_crypto_pwhash_str_verify') && @sodium_crypto_pwhash_str_verify($encoded, $password);
    }

    public function needsRehash(#[\SensitiveParameter] string $encoded): bool
    {
        $current = sprintf('$argon2id$v=19$m=%d,t=%d,p=%d$', self::MEMORY_KIB, self::PASSES, self::LANES);

        return !str_starts_with($encoded, $current) || !self::isBoundedHash($encoded);
    }

    private static function nativeArgon(): bool
    {
        return function_exists('password_algos') && in_array('argon2id', password_algos(), true);
    }

    private static function sodiumCanHash(): bool
    {
        return function_exists('sodium_crypto_pwhash') && defined('SODIUM_CRYPTO_PWHASH_ALG_ARGON2ID13');
    }

    private static function isBoundedHash(string $encoded): bool
    {
        // Unpadded Base64 lengths of 11–86 and 22–86 represent 8–64 salt bytes and 16–64 key bytes.
        $pattern = '~\A\$argon2id\$v=19\$m=([1-9][0-9]{0,5}),t=([1-9][0-9]?),p=([1-9][0-9]?)\$([A-Za-z0-9+/]{11,86})\$([A-Za-z0-9+/]{22,86})\z~';
        if (preg_match($pattern, $encoded, $parts) !== 1) {
            return false;
        }

        $memory = (int) $parts[1];
        $passes = (int) $parts[2];
        $lanes = (int) $parts[3];

        return $memory >= 8 * $lanes
            && $memory <= 128 * 1024
            && $passes <= 10
            && $lanes <= 16
            && self::isCanonicalBase64($parts[4])
            && self::isCanonicalBase64($parts[5]);
    }

    private static function isCanonicalBase64(string $encoded): bool
    {
        $bytes = base64_decode($encoded, true);

        return $bytes !== false && rtrim(base64_encode($bytes), '=') === $encoded;
    }
}
