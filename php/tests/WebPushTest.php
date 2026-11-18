<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Base64Url\Base64Url;
use CurlHandle;
use DateTimeImmutable;
use InvalidArgumentException;
use Jose\Component\Core\JWK;
use Jose\Component\Signature\Algorithm\ES256;
use Minishlink\WebPush\ContentEncoding;
use Minishlink\WebPush\Encryption;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Http\Client\ClientExceptionInterface;
use Psr\Http\Client\ClientInterface;
use Psr\Http\Message\RequestInterface;
use Psr\Http\Message\ResponseInterface;
use RuntimeException;
use Slim\Psr7\Factory\RequestFactory;
use Slim\Psr7\Factory\ResponseFactory;
use Slim\Psr7\Factory\StreamFactory;
use StudioSchedule\Push\CurlPushClient;
use StudioSchedule\Push\Sender;
use StudioSchedule\Push\StatusError;
use StudioSchedule\Push\TransportError;
use StudioSchedule\Push\VapidKeys;

final class WebPushTest extends TestCase
{
    // RFC 8291 section 5 and appendix A, shared with internal/webpush/encrypt_test.go.
    private const BROWSER_PUBLIC = 'BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4';
    private const BROWSER_PRIVATE = 'q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94';
    private const AUTH = 'BTBZMqHH6r4Tts7J_aSIgg';
    private const SERVER_PUBLIC = 'BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8';
    private const SERVER_PRIVATE = 'yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw';
    private const SALT = 'DGv6ra1nlYgDCS1FRnbzlw';
    private const RFC_PAYLOAD = 'When I grow up, I want to be a watermelon';
    private const RFC_BODY = 'DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN';
    private const ENDPOINT = 'https://push.example.net:8443/push/abc?token=opaque';
    private const SUBJECT = 'mailto:office@example.org';

    public function testSendsBrowserDecryptableRecordsWithVapid(): void
    {
        $request = null;
        $client = self::createStub(ClientInterface::class);
        $client->method('sendRequest')->willReturnCallback(function (RequestInterface $sent) use (&$request): ResponseInterface {
            $request = $sent;

            return (new ResponseFactory())->createResponse(201);
        });
        $clock = self::createStub(ClockInterface::class);
        $clock->method('now')->willReturn(new DateTimeImmutable('2026-01-01T09:00:00Z'));

        $sender = new Sender($this->keys(), self::SUBJECT, $clock, $client);
        $sender->send(self::ENDPOINT, self::BROWSER_PUBLIC, self::AUTH, self::RFC_PAYLOAD, 5400, 'high');
        $sent = $request;
        self::assertInstanceOf(RequestInterface::class, $sent);
        self::assertSame('POST', $sent->getMethod());
        self::assertSame(self::ENDPOINT, (string) $sent->getUri());
        self::assertSame('push.example.net:8443', $sent->getHeaderLine('Host'));
        self::assertSame('5400', $sent->getHeaderLine('TTL'));
        self::assertSame('high', $sent->getHeaderLine('Urgency'));
        self::assertSame('application/octet-stream', $sent->getHeaderLine('Content-Type'));
        self::assertSame('aes128gcm', $sent->getHeaderLine('Content-Encoding'));
        $body = (string) $sent->getBody();
        self::assertSame(self::RFC_PAYLOAD, self::decrypt($body));
        self::assertSame(self::SERVER_PUBLIC, $sender->publicKey());
        self::assertSame(1, preg_match('/^vapid t=([^,]+), k=(.+)$/', $sent->getHeaderLine('Authorization'), $match));
        self::assertSame(self::SERVER_PUBLIC, $match[2] ?? null);
        $parts = explode('.', $match[1]);
        self::assertCount(3, $parts);
        self::assertSame(['typ' => 'JWT', 'alg' => 'ES256'], json_decode(Base64Url::decode($parts[0]), true, flags: JSON_THROW_ON_ERROR));
        self::assertSame([
            'aud' => 'https://push.example.net:8443',
            'exp' => 1767301200,
            'sub' => self::SUBJECT,
        ], json_decode(Base64Url::decode($parts[1]), true, flags: JSON_THROW_ON_ERROR));
        $signature = Base64Url::decode($parts[2]);
        self::assertSame(64, strlen($signature));
        self::assertTrue((new ES256())->verify($this->jwk(self::SERVER_PUBLIC), $parts[0] . '.' . $parts[1], $signature));

        $sender->send(self::ENDPOINT, self::BROWSER_PUBLIC, self::AUTH, '', -50);
        $sent = $request;
        self::assertInstanceOf(RequestInterface::class, $sent);
        self::assertSame('0', $sent->getHeaderLine('TTL'));
        self::assertSame('', $sent->getHeaderLine('Urgency'));
        self::assertSame('', self::decrypt((string) $sent->getBody()));
        self::assertNotSame(substr($body, 0, 86), substr((string) $sent->getBody(), 0, 86));
    }

    public function testMatchesTheDeterministicRfc8291Example(): void
    {
        $encoding = ContentEncoding::aes128gcm;
        /** @var array{salt:string,localPublicKey:string,cipherText:string} $encrypted */
        $encrypted = Encryption::deterministicEncrypt(
            Encryption::padPayload(self::RFC_PAYLOAD, 0, $encoding),
            self::BROWSER_PUBLIC,
            self::AUTH,
            $encoding,
            [$this->jwk(self::SERVER_PUBLIC, self::SERVER_PRIVATE)],
            Base64Url::decode(self::SALT),
        );
        $body = Encryption::getContentCodingHeader($encrypted['salt'], $encrypted['localPublicKey'], $encoding) . $encrypted['cipherText'];
        self::assertSame(self::RFC_BODY, Base64Url::encode($body));
        self::assertSame(self::RFC_PAYLOAD, self::decrypt(Base64Url::decode(self::RFC_BODY)));
    }

    public function testAcceptsTheFull4096ByteRecord(): void
    {
        $client = self::createMock(ClientInterface::class);
        $client->expects(self::once())->method('sendRequest')->willReturnCallback(static function (RequestInterface $request): ResponseInterface {
            $body = (string) $request->getBody();
            self::assertSame(4096, strlen($body));
            self::assertSame(str_repeat('x', 3993), self::decrypt($body));

            return (new ResponseFactory())->createResponse(204);
        });
        $sender = new Sender($this->keys(), 'https://schedule.example.org/contact', client: $client);
        $sender->send(self::ENDPOINT, self::BROWSER_PUBLIC, self::AUTH, str_repeat('x', 3993));
    }

    public function testRejectsOversizedPayloadsBeforeAnyRequest(): void
    {
        $client = self::createMock(ClientInterface::class);
        $client->expects(self::never())->method('sendRequest');
        $sender = new Sender($this->keys(), self::SUBJECT, client: $client);

        $this->expectException(InvalidArgumentException::class);
        $sender->send(self::ENDPOINT, self::BROWSER_PUBLIC, self::AUTH, str_repeat('x', 3994));
    }

    #[DataProvider('invalidSubscriptions')]
    public function testRejectsMalformedSubscriptionsBeforeAnyRequest(string $endpoint, string $key, string $auth): void
    {
        $client = self::createMock(ClientInterface::class);
        $client->expects(self::never())->method('sendRequest');
        $sender = new Sender($this->keys(), self::SUBJECT, client: $client);

        $this->expectException(InvalidArgumentException::class);
        $sender->send($endpoint, $key, $auth, '{}');
    }

    /** @return iterable<string,array{string,string,string}> */
    public static function invalidSubscriptions(): iterable
    {
        yield 'insecure URL' => ['http://push.example.net/abc', self::BROWSER_PUBLIC, self::AUTH];
        yield 'missing host' => ['https:///abc', self::BROWSER_PUBLIC, self::AUTH];
        yield 'credentials' => ['https://user:password@push.example.net/abc', self::BROWSER_PUBLIC, self::AUTH];
        yield 'invalid escape' => ['https://push.example.net/%zz', self::BROWSER_PUBLIC, self::AUTH];
        yield 'literal scheme' => ['HTTPS://push.example.net/abc', self::BROWSER_PUBLIC, self::AUTH];
        yield 'too long' => ['https://push.example.net/' . str_repeat('x', 2048), self::BROWSER_PUBLIC, self::AUTH];
        yield 'invalid public key' => [self::ENDPOINT, '!!', self::AUTH];
        yield 'invalid prefix' => [self::ENDPOINT, Base64Url::encode(str_repeat("\0", 65)), self::AUTH];
        yield 'point not on the curve' => [self::ENDPOINT, Base64Url::encode("\x04" . str_repeat("\0", 31) . "\x01" . str_repeat("\0", 31) . "\x01"), self::AUTH];
        yield 'invalid authentication secret' => [self::ENDPOINT, self::BROWSER_PUBLIC, 'AA'];
    }

    #[DataProvider('refusals')]
    public function testClassifiesPushServiceRefusals(int $status, bool $gone, bool $permanent): void
    {
        $response = (new ResponseFactory())->createResponse($status)->withHeader('Location', 'https://different.example.org/path');
        $body = (new StreamFactory())->createStream("\t" . str_repeat('e', 510) . ' ' . str_repeat('z', 100));
        $client = self::createMock(ClientInterface::class);
        $client->expects(self::once())->method('sendRequest')->willReturn($response->withBody($body));
        $sender = new Sender($this->keys(), self::SUBJECT, client: $client);
        try {
            $sender->send(self::ENDPOINT, self::BROWSER_PUBLIC, self::AUTH, '{}');
            self::fail('Expected a non-success status');
        } catch (StatusError $error) {
            self::assertSame($status, $error->statusCode);
            self::assertSame(str_repeat('e', 510), $error->body);
            self::assertSame($gone, $error->gone());
            self::assertSame($permanent, $error->permanent());
            self::assertTrue(str_starts_with($error->getMessage(), 'webpush: push service answered ' . $status));
        }
    }

    /** @return iterable<string,array{int,bool,bool}> */
    public static function refusals(): iterable
    {
        yield 'redirect' => [307, false, false];
        yield 'bad request' => [400, false, true];
        yield 'unauthorized' => [401, false, true];
        yield 'forbidden' => [403, false, true];
        yield 'missing subscription' => [404, true, true];
        yield 'expired subscription' => [410, true, true];
        yield 'large request' => [413, false, true];
        yield 'rate limit' => [429, false, false];
        yield 'temporarily unavailable' => [503, false, false];
    }

    public function testParsesPaddedKeysAndGeneratesAnInteroperableKeyPair(): void
    {
        $padded = VapidKeys::parse(' ' . self::SERVER_PUBLIC . '= ', self::SERVER_PRIVATE . '=');
        self::assertSame(self::SERVER_PUBLIC, $padded->publicKey());
        self::assertSame(self::SERVER_PRIVATE, $padded->privateKey());

        $generated = VapidKeys::generate();
        self::assertSame(65, strlen(Base64Url::decode($generated->publicKey())));
        self::assertSame(32, strlen(Base64Url::decode($generated->privateKey())));
        self::assertSame($generated->publicKey(), VapidKeys::parse($generated->publicKey(), $generated->privateKey())->publicKey());
    }

    #[DataProvider('invalidKeyPairs')]
    public function testRejectsInvalidVapidKeys(string $public, string $private): void
    {
        $this->expectException(InvalidArgumentException::class);
        VapidKeys::parse($public, $private);
    }

    /** @return iterable<string,array{string,string}> */
    public static function invalidKeyPairs(): iterable
    {
        yield 'different public key' => [self::BROWSER_PUBLIC, self::SERVER_PRIVATE];
        yield 'public key is malformed' => ['!!', self::SERVER_PRIVATE];
        yield 'scalar is malformed' => [self::SERVER_PUBLIC, 'AAAA'];
        yield 'scalar is zero' => [self::SERVER_PUBLIC, Base64Url::encode(str_repeat("\0", 32))];
        yield 'scalar exceeds curve order' => [self::SERVER_PUBLIC, Base64Url::encode(str_repeat("\xff", 32))];
    }

    #[DataProvider('invalidSubjects')]
    public function testRejectsInvalidContactSubjects(string $subject): void
    {
        $this->expectException(InvalidArgumentException::class);
        new Sender($this->keys(), $subject, client: self::createStub(ClientInterface::class));
    }

    /** @return iterable<string,array{string}> */
    public static function invalidSubjects(): iterable
    {
        yield 'bare email' => ['office@example.org'];
        yield 'missing email' => ['mailto:'];
        yield 'bad mailto form' => ['mailto://office@example.org'];
        yield 'insecure URL' => ['http://schedule.example.org'];
        yield 'missing host' => ['https://'];
        yield 'invalid escape' => ['https://schedule.example.org/%zz'];
    }

    public function testNetworkFailureDoesNotExposeTheCapabilityUrl(): void
    {
        $client = self::createStub(ClientInterface::class);
        $exception = new class (self::ENDPOINT) extends RuntimeException implements ClientExceptionInterface {};
        $client->method('sendRequest')->willThrowException($exception);
        $sender = new Sender($this->keys(), self::SUBJECT, client: $client);

        try {
            $sender->send(self::ENDPOINT, self::BROWSER_PUBLIC, self::AUTH, '{}');
            self::fail('Expected a transport failure');
        } catch (RuntimeException $error) {
            self::assertSame('webpush: transport failed', $error->getMessage());
            self::assertNull($error->getPrevious());
        }
    }

    public function testDefaultClientRejectsPrivateEndpointsBeforeConnecting(): void
    {
        $sender = new Sender($this->keys(), self::SUBJECT);
        $this->expectException(TransportError::class);
        $this->expectExceptionMessage('endpoint is not a public address');
        $sender->send('https://[::ffff:127.0.0.1]:8443/private', self::BROWSER_PUBLIC, self::AUTH, '{}');
    }

    public function testDefaultClientPassesThroughARefusalAndReportsTransportFailures(): void
    {
        $endpoint = 'https://192.0.2.1:8443/push';
        $request = (new RequestFactory())->createRequest('POST', $endpoint)->withBody((new StreamFactory())->createStream('encrypted'));
        $client = new CurlPushClient(static function (CurlHandle $handle) use ($endpoint): array {
            self::assertSame($endpoint, curl_getinfo($handle, CURLINFO_EFFECTIVE_URL));

            return ['status' => 307, 'error' => CURLE_PARTIAL_FILE];
        });
        self::assertSame(307, $client->sendRequest($request)->getStatusCode());

        $client = new CurlPushClient(static fn(CurlHandle $_): array => ['status' => 0, 'error' => CURLE_COULDNT_CONNECT]);
        $this->expectException(TransportError::class);
        $this->expectExceptionMessage('webpush: transport failed');
        $client->sendRequest($request);
    }

    #[DataProvider('networkAddresses')]
    public function testOnlyPublicUnicastAddressesAreAccepted(string $address, bool $public): void
    {
        self::assertSame($public, CurlPushClient::isPublicAddress($address));
    }

    /** @return iterable<string,array{string,bool}> */
    public static function networkAddresses(): iterable
    {
        foreach (['142.250.185.74', '2a00:1450:4001:82b::200a', '203.0.113.7', '2001:db8::1', '::ffff:142.250.185.74'] as $address) {
            yield $address => [$address, true];
        }
        foreach (['127.0.0.1', '::1', '10.1.2.3', '172.16.0.1', '192.168.1.1', '169.254.169.254', 'fe80::1', 'fc00::1', '100.64.0.1', '0.0.0.0', '::ffff:127.0.0.1', '224.0.0.1', 'ff02::1', 'push.example.org'] as $address) {
            yield $address => [$address, false];
        }
    }

    private function keys(): VapidKeys
    {
        return VapidKeys::parse(self::SERVER_PUBLIC, self::SERVER_PRIVATE);
    }

    private function jwk(string $public, ?string $private = null): JWK
    {
        $point = Base64Url::decode($public);
        $jwk = ['kty' => 'EC', 'crv' => 'P-256', 'x' => Base64Url::encode(substr($point, 1, 32)), 'y' => Base64Url::encode(substr($point, 33, 32))];
        if ($private !== null) {
            $jwk['d'] = $private;
        }

        return new JWK($jwk);
    }

    /** Independent browser-side decryption of a one-record RFC 8291 body. */
    private static function decrypt(string $body): string
    {
        $salt = substr($body, 0, 16);
        self::assertSame("\0\0\x10\0\x41", substr($body, 16, 5));
        $ephemeral = substr($body, 21, 65);
        $browser = openssl_pkey_new(['ec' => ['curve_name' => 'prime256v1', 'd' => Base64Url::decode(self::BROWSER_PRIVATE)]]);
        self::assertNotFalse($browser);
        $server = openssl_pkey_new(['ec' => ['curve_name' => 'prime256v1', 'x' => substr($ephemeral, 1, 32), 'y' => substr($ephemeral, 33, 32)]]);
        self::assertNotFalse($server);
        $secret = openssl_pkey_derive($server, $browser);
        self::assertNotFalse($secret);
        $ikm = hash_hkdf('sha256', $secret, 32, "WebPush: info\0" . Base64Url::decode(self::BROWSER_PUBLIC) . $ephemeral, Base64Url::decode(self::AUTH));
        $cek = hash_hkdf('sha256', $ikm, 16, "Content-Encoding: aes128gcm\0", $salt);
        $nonce = hash_hkdf('sha256', $ikm, 12, "Content-Encoding: nonce\0", $salt);
        $plain = openssl_decrypt(substr($body, 86, -16), 'aes-128-gcm', $cek, OPENSSL_RAW_DATA, $nonce, substr($body, -16));
        self::assertNotFalse($plain);
        self::assertStringEndsWith("\x02", $plain);

        return substr($plain, 0, -1);
    }
}
