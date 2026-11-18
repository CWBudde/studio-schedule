<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Closure;
use InvalidArgumentException;
use JsonException;
use Psr\Clock\ClockInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use stdClass;
use StudioSchedule\Push\KeyEncoding;
use StudioSchedule\Push\Sender;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Store\PushSubscriptionStore;

final readonly class PushRoutes
{
    private const int MAX_BODY_BYTES = 1024 * 1024;

    /** @param Closure():?Sender $sender */
    public function __construct(private PushSubscriptionStore $store, private ClockInterface $clock, private Closure $sender) {}

    public function __invoke(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $method = $request->getMethod();
        // Slim also dispatches HEAD to the GET route when it has no HEAD handler.
        if (!in_array($method, ['GET', 'POST', 'DELETE'], true)) {
            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', 'GET');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }

        if ($method === 'GET') {
            $sender = ($this->sender)();

            return Responses::json($response, $sender === null ? ['enabled' => false] : ['enabled' => true, 'publicKey' => $sender->publicKey()]);
        }

        $body = self::body($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $fields = array_change_key_case(get_object_vars($body));
        $allowed = $method === 'DELETE' ? ['endpoint'] : ['endpoint', 'keys', 'devicelabel'];
        if (array_diff(array_keys($fields), $allowed) !== []) {
            return self::invalid($request, $response, 'body', 'unexpected property');
        }
        $endpoint = $fields['endpoint'] ?? null;
        if (!is_string($endpoint) || $endpoint === '' || mb_strlen($endpoint, 'UTF-8') > 2048) {
            return self::invalid($request, $response, 'body.endpoint', 'expected an endpoint of 1 to 2048 characters');
        }
        if ($method === 'DELETE') {
            $this->store->delete($principal->accountId, $endpoint);

            return $response->withStatus(204);
        }

        return $this->save($request, $response, $principal, $endpoint, $fields);
    }

    /** @param array<array-key,mixed> $fields */
    private function save(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        SessionPrincipal $principal,
        #[SensitiveParameter]
        string $endpoint,
        #[SensitiveParameter]
        array $fields,
    ): ResponseInterface {
        $keys = $fields['keys'] ?? null;
        if (!$keys instanceof stdClass) {
            return self::invalid($request, $response, 'body.keys', 'expected an object');
        }
        $keys = array_change_key_case(get_object_vars($keys));
        if (array_diff(array_keys($keys), ['p256dh', 'auth']) !== []) {
            return self::invalid($request, $response, 'body.keys', 'unexpected property');
        }
        $p256dh = $keys['p256dh'] ?? null;
        $auth = $keys['auth'] ?? null;
        if (!is_string($p256dh) || $p256dh === '' || mb_strlen($p256dh, 'UTF-8') > 100) {
            return self::invalid($request, $response, 'body.keys.p256dh', 'expected a key of 1 to 100 characters');
        }
        if (!is_string($auth) || $auth === '' || mb_strlen($auth, 'UTF-8') > 40) {
            return self::invalid($request, $response, 'body.keys.auth', 'expected a key of 1 to 40 characters');
        }
        $label = $fields['devicelabel'] ?? '';
        if (!is_string($label) || mb_strlen($label, 'UTF-8') > 100 || str_contains($label, "\0")) {
            return self::invalid($request, $response, 'body.deviceLabel', 'expected a device label of at most 100 characters');
        }
        if (($this->sender)() === null) {
            return Responses::problem($request, 404, 'not found');
        }
        if ($principal->impersonatorId !== null) {
            return Responses::problem($request, 403, 'not while signed in as another account');
        }

        try {
            $publicKey = KeyEncoding::browserPublicKey($p256dh);
        } catch (InvalidArgumentException) {
            return self::invalid($request, $response, 'body.keys.p256dh', 'p256dh must be the base64url of a P-256 public key');
        }
        try {
            $secret = KeyEncoding::decode($auth, 16);
        } catch (InvalidArgumentException) {
            return self::invalid($request, $response, 'body.keys.auth', 'auth must be the base64url of 16 bytes');
        }
        try {
            Sender::validateEndpoint($endpoint);
        } catch (InvalidArgumentException) {
            return self::invalid($request, $response, 'body.endpoint', 'the endpoint must be an https URL');
        }
        if ($secret === str_repeat("\0", 16)) {
            return self::invalid($request, $response, 'body.keys.auth', 'auth must be set');
        }
        $label = preg_replace('/^\s+|\s+$/u', '', $label);
        $this->store->save($principal->accountId, $endpoint, $publicKey, $secret, $label === '' ? null : $label, $this->clock->now());

        return $response->withStatus(204);
    }

    private static function body(#[SensitiveParameter] ServerRequestInterface $request): stdClass|ResponseInterface
    {
        $stream = $request->getBody();
        $body = '';
        while (!$stream->eof() && strlen($body) < self::MAX_BODY_BYTES) {
            $chunk = $stream->read(self::MAX_BODY_BYTES - strlen($body));
            if ($chunk === '') {
                break;
            }
            $body .= $chunk;
        }
        if (strlen($body) >= self::MAX_BODY_BYTES) {
            return Responses::problem($request, 413, 'request body is too large limit=' . self::MAX_BODY_BYTES . ' bytes');
        }
        if ($body === '') {
            return Responses::problem($request, 400, 'request body is required');
        }
        $mediaType = strtolower(trim(explode(';', $request->getHeaderLine('Content-Type'), 2)[0]));
        if ($mediaType !== '' && $mediaType !== 'application/json' && !str_ends_with($mediaType, '+json')) {
            return Responses::problem($request, 415, 'unsupported content type');
        }

        try {
            $decoded = json_decode($body, flags: JSON_THROW_ON_ERROR);
        } catch (JsonException) {
            return Responses::problem($request, 400, 'invalid JSON');
        }

        return $decoded instanceof stdClass ? $decoded : Responses::problem($request, 422, 'expected an object');
    }

    private static function invalid(ServerRequestInterface $request, ResponseInterface $response, string $field, string $message): ResponseInterface
    {
        $response = $response->withStatus(422);
        $body = ['title' => $response->getReasonPhrase(), 'status' => 422, 'detail' => 'validation failed',
            'errors' => [['location' => $field, 'message' => $message]]];
        $requestId = $request->getAttribute('requestId');
        if (is_string($requestId)) {
            $body['requestId'] = $requestId;
        }

        return Responses::json($response, $body)->withHeader('Content-Type', 'application/problem+json')->withHeader('Cache-Control', 'no-store');
    }
}
