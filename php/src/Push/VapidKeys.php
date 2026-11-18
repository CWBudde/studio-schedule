<?php

declare(strict_types=1);

namespace StudioSchedule\Push;

use Base64Url\Base64Url;
use InvalidArgumentException;
use Minishlink\WebPush\ContentEncoding;
use Minishlink\WebPush\VAPID;
use SensitiveParameter;

/** Application server keys in the same raw P-256 format as the Go backend. */
final readonly class VapidKeys
{
    private const CURVE_ORDER = 'ffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551';

    private function __construct(private string $public, #[SensitiveParameter] private string $private) {}

    public static function generate(): self
    {
        /** @var array{publicKey:string,privateKey:string} $keys */
        $keys = VAPID::createVapidKeys();

        return self::parse($keys['publicKey'], $keys['privateKey']);
    }

    public static function parse(string $public, #[SensitiveParameter] string $private): self
    {
        $point = KeyEncoding::decode($public, 65);
        $scalar = KeyEncoding::decode($private, 32);
        if ($scalar === str_repeat("\0", 32) || strcmp(bin2hex($scalar), self::CURVE_ORDER) >= 0) {
            throw new InvalidArgumentException('webpush: invalid VAPID private scalar');
        }

        $handle = openssl_pkey_new(['ec' => ['curve_name' => KeyEncoding::CURVE, 'd' => $scalar]]);
        $details = $handle === false ? false : openssl_pkey_get_details($handle);
        $parameters = $details['ec'] ?? null;
        $x = is_array($parameters) ? ($parameters['x'] ?? null) : null;
        $y = is_array($parameters) ? ($parameters['y'] ?? null) : null;
        if (!is_string($x) || !is_string($y) || $point !== "\x04" . str_pad($x, 32, "\0", STR_PAD_LEFT) . str_pad($y, 32, "\0", STR_PAD_LEFT)) {
            throw new InvalidArgumentException('webpush: VAPID public and private keys do not match');
        }

        return new self($point, $scalar);
    }

    public function publicKey(): string
    {
        return Base64Url::encode($this->public);
    }

    public function privateKey(): string
    {
        return Base64Url::encode($this->private);
    }

    /** @internal */
    public function authorization(string $audience, string $subject, int $expires): string
    {
        /** @var array{Authorization:string} $headers */
        $headers = VAPID::getVapidHeaders($audience, $subject, $this->public, $this->private, ContentEncoding::aes128gcm, $expires);

        return $headers['Authorization'];
    }
}
