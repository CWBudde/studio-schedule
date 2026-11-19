<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use JsonException;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use stdClass;

final class JsonObject
{
    private const int MAX_BODY_BYTES = 1024 * 1024;

    public static function read(#[SensitiveParameter] ServerRequestInterface $request, int $maxBytes = self::MAX_BODY_BYTES): stdClass|ResponseInterface
    {
        $stream = $request->getBody();
        $body = '';
        while (!$stream->eof() && strlen($body) < $maxBytes) {
            $chunk = $stream->read($maxBytes - strlen($body));
            if ($chunk === '') {
                break;
            }
            $body .= $chunk;
        }
        if (strlen($body) >= $maxBytes) {
            return Responses::problem($request, 413, 'request body is too large limit=' . $maxBytes . ' bytes');
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
}
