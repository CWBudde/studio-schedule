<?php

declare(strict_types=1);

namespace StudioSchedule\Push;

use Base64Url\Base64Url;
use InvalidArgumentException;
use Minishlink\WebPush\ContentEncoding;
use Minishlink\WebPush\Encryption;
use Psr\Clock\ClockInterface;
use Psr\Http\Client\ClientExceptionInterface;
use Psr\Http\Client\ClientInterface;
use RuntimeException;
use SensitiveParameter;
use Slim\Psr7\Factory\RequestFactory;
use Slim\Psr7\Factory\StreamFactory;
use StudioSchedule\Service\SystemClock;

final readonly class Sender
{
    // One 4096-byte record, minus the 86-byte header, delimiter and 16-byte tag.
    public const MAX_PAYLOAD = 3993;
    public const MAX_RESPONSE_BODY = 512;

    public function __construct(
        private VapidKeys $keys,
        private string $subject,
        private ClockInterface $clock = new SystemClock(),
        private ClientInterface $client = new CurlPushClient(),
    ) {
        self::validateSubject($subject);
    }

    public function publicKey(): string
    {
        return $this->keys->publicKey();
    }

    public static function validateEndpoint(#[SensitiveParameter] string $endpoint): void
    {
        self::endpointOrigin($endpoint);
    }

    public static function validateSubject(string $subject): void
    {
        $parts = parse_url($subject);
        if (
            !self::hasMalformedCharacters($subject)
            && (
                (($parts['scheme'] ?? null) === 'mailto' && !isset($parts['host']) && str_contains($parts['path'] ?? '', '@'))
                || (str_starts_with($subject, 'https://') && filter_var($subject, FILTER_VALIDATE_URL) !== false)
            )
        ) {
            return;
        }

        throw new InvalidArgumentException('webpush: VAPID subject must be a mailto: or https: URL');
    }

    /** @throws StatusError When the push service answers with a non-2xx status. */
    public function send(
        #[SensitiveParameter]
        string $endpoint,
        string $p256dh,
        #[SensitiveParameter]
        string $auth,
        #[SensitiveParameter]
        string $payload,
        int $ttlSeconds = 0,
        string $urgency = '',
    ): void {
        $origin = self::endpointOrigin($endpoint);
        if (strlen($payload) > self::MAX_PAYLOAD) {
            throw new InvalidArgumentException('webpush: payload exceeds 3993 bytes');
        }

        $point = KeyEncoding::decode($p256dh, 65);
        $auth = KeyEncoding::decode($auth, 16);
        if ($point[0] !== "\x04" || openssl_pkey_new(['ec' => [
            'curve_name' => KeyEncoding::CURVE,
            'x' => substr($point, 1, 32),
            'y' => substr($point, 33, 32),
        ]]) === false) {
            throw new InvalidArgumentException('webpush: browser key is not an uncompressed P-256 point');
        }

        $encoding = ContentEncoding::aes128gcm;
        /** @var array{salt:string,localPublicKey:string,cipherText:string} $encrypted */
        $encrypted = Encryption::encrypt(Encryption::padPayload($payload, 0, $encoding), Base64Url::encode($point), Base64Url::encode($auth), $encoding);
        $body = Encryption::getContentCodingHeader($encrypted['salt'], $encrypted['localPublicKey'], $encoding) . $encrypted['cipherText'];
        $request = (new RequestFactory())->createRequest('POST', $endpoint)
            ->withHeader('Host', substr($origin, strlen('https://')))
            ->withHeader('Authorization', $this->keys->authorization($origin, $this->subject, $this->clock->now()->getTimestamp() + 43200))
            ->withHeader('Content-Encoding', $encoding->value)
            ->withHeader('Content-Type', 'application/octet-stream')
            ->withHeader('TTL', (string) max(0, $ttlSeconds))
            ->withBody((new StreamFactory())->createStream($body));

        if ($urgency !== '') {
            $request = $request->withHeader('Urgency', $urgency);
        }

        try {
            $response = $this->client->sendRequest($request);
        } catch (TransportError $error) {
            throw $error;
        } catch (ClientExceptionInterface) {
            // Client exceptions may include the subscription's capability URL.
            throw new RuntimeException('webpush: transport failed');
        }
        $status = $response->getStatusCode();
        $responseBody = $response->getBody();
        $detail = '';
        try {
            if ($status >= 200 && $status < 300) {
                return;
            }
            while (strlen($detail) < self::MAX_RESPONSE_BODY && !$responseBody->eof()) {
                $chunk = $responseBody->read(self::MAX_RESPONSE_BODY - strlen($detail));
                if ($chunk === '') {
                    break;
                }
                $detail .= $chunk;
            }
        } catch (RuntimeException) {
            // Preserve the HTTP status when a diagnostic body cannot be read.
        } finally {
            $responseBody->close();
        }

        throw new StatusError($status, trim($detail), $response->getReasonPhrase());
    }

    private static function endpointOrigin(#[SensitiveParameter] string $endpoint): string
    {
        $url = parse_url($endpoint);
        if (
            strlen($endpoint) > 2048 || !str_starts_with($endpoint, 'https://') || self::hasMalformedCharacters($endpoint)
            || filter_var($endpoint, FILTER_VALIDATE_URL) === false || !isset($url['host']) || isset($url['user'])
        ) {
            throw new InvalidArgumentException('webpush: endpoint must be an HTTPS URL without credentials');
        }

        return 'https://' . $url['host'] . (isset($url['port']) ? ':' . $url['port'] : '');
    }

    private static function hasMalformedCharacters(#[SensitiveParameter] string $url): bool
    {
        return preg_match('/[\x00-\x20\x7f]|%(?![0-9a-f]{2})/i', $url) === 1 || str_contains($url, '\\');
    }
}
