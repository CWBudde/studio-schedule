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

    public static function read(#[SensitiveParameter] ServerRequestInterface $request): stdClass|ResponseInterface
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
}
