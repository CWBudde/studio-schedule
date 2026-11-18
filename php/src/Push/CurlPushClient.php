<?php

declare(strict_types=1);

namespace StudioSchedule\Push;

use Closure;
use CurlHandle;
use InvalidArgumentException;
use Psr\Http\Client\ClientInterface;
use Psr\Http\Message\RequestInterface;
use Psr\Http\Message\ResponseInterface;
use SensitiveParameter;
use Slim\Psr7\Factory\ResponseFactory;
use Slim\Psr7\Factory\StreamFactory;

/** Sends Web Push requests directly to vetted public IPs. */
final class CurlPushClient implements ClientInterface
{
    /** @param (Closure(CurlHandle): array{status:int,error:int})|null $transfer */
    public function __construct(private readonly ?Closure $transfer = null) {}

    public function sendRequest(#[SensitiveParameter] RequestInterface $request): ResponseInterface
    {
        $started = hrtime(true);
        $uri = $request->getUri();
        try {
            Sender::validateEndpoint((string) $uri);
        } catch (InvalidArgumentException) {
            throw new TransportError('webpush: invalid endpoint');
        }
        if ($request->getMethod() !== 'POST') {
            throw new TransportError('webpush: a push request must use POST');
        }
        $port = $uri->getPort() ?? 443;
        $ips = self::resolve(trim($uri->getHost(), '[]'));
        $timeout = 30_000 - (int) ((hrtime(true) - $started) / 1_000_000);
        if ($timeout <= 0) {
            throw new TransportError('webpush: endpoint resolution timed out');
        }

        $handle = curl_init((string) $uri);
        if ($handle === false) {
            throw new TransportError('webpush: could not initialize cURL');
        }

        $body = '';
        $headers = [];
        foreach ($request->getHeaders() as $name => $values) {
            foreach ($values as $value) {
                $headers[] = $name . ': ' . $value;
            }
        }

        // Route through a private cURL DNS cache entry, keeping the URL's host for TLS and HTTP.
        $configured = curl_setopt_array($handle, [
            CURLOPT_CONNECT_TO => ['::push-address.invalid:' . $port],
            CURLOPT_RESOLVE => ['push-address.invalid:' . $port . ':' . implode(',', $ips)],
            CURLOPT_PROXY => '',
            CURLOPT_FOLLOWLOCATION => false,
            CURLOPT_PATH_AS_IS => true,
            CURLOPT_PROTOCOLS => CURLPROTO_HTTPS,
            CURLOPT_CONNECTTIMEOUT_MS => min(10_000, $timeout),
            CURLOPT_TIMEOUT_MS => $timeout,
            CURLOPT_HTTPHEADER => $headers,
            CURLOPT_POSTFIELDS => (string) $request->getBody(),
            CURLOPT_WRITEFUNCTION => static function (CurlHandle $_, string $chunk) use (&$body): int {
                $remaining = Sender::MAX_RESPONSE_BODY - strlen($body);
                $body .= substr($chunk, 0, $remaining);

                return strlen($chunk) >= $remaining ? 0 : strlen($chunk);
            },
        ]);
        if (!$configured) {
            throw new TransportError('webpush: could not configure cURL');
        }

        ['status' => $status, 'error' => $error] = ($this->transfer ?? self::execute(...))($handle);
        // Preserve the final HTTP status even if reading the diagnostic body was interrupted.
        if ($status < 200) {
            throw new TransportError('webpush: transport failed (cURL ' . $error . ')');
        }

        return (new ResponseFactory())->createResponse($status)->withBody((new StreamFactory())->createStream($body));
    }

    /** @return array{status:int,error:int} */
    private static function execute(CurlHandle $handle): array
    {
        curl_exec($handle);

        return ['status' => (int) curl_getinfo($handle, CURLINFO_RESPONSE_CODE), 'error' => curl_errno($handle)];
    }

    public static function isPublicAddress(string $address): bool
    {
        $bytes = inet_pton($address);
        if ($bytes === false) {
            return false;
        }
        if (str_starts_with($bytes, str_repeat("\0", 10) . "\xff\xff")) {
            $bytes = substr($bytes, 12);
            $address = implode('.', array_map(ord(...), str_split($bytes)));
        }

        if (filter_var($address, FILTER_VALIDATE_IP, FILTER_FLAG_NO_PRIV_RANGE | FILTER_FLAG_NO_RES_RANGE) === false) {
            return false;
        }
        if (strlen($bytes) === 4) {
            return ord($bytes[0]) < 224 && !(ord($bytes[0]) === 100 && (ord($bytes[1]) & 0xc0) === 0x40);
        }

        return $bytes[0] !== "\xff";
    }

    /** @return non-empty-list<string> */
    private static function resolve(string $host): array
    {
        $addresses = [];
        if (filter_var($host, FILTER_VALIDATE_IP) !== false) {
            $addresses[] = $host;
        } else {
            foreach (@dns_get_record($host, DNS_A | DNS_AAAA) ?: [] as $record) {
                $address = $record['ip'] ?? $record['ipv6'] ?? null;
                if (is_string($address)) {
                    $addresses[] = $address;
                }
            }
        }
        $public = [];
        foreach ($addresses as $address) {
            if (self::isPublicAddress($address)) {
                $public[] = str_contains($address, ':') ? '[' . $address . ']' : $address;
            }
        }
        if ($public === []) {
            throw new TransportError('webpush: the endpoint is not a public address');
        }

        return $public;
    }
}
