<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Slim\Psr7\Response;

final class Responses
{
    private const CSP = "default-src 'self'; img-src 'self' data:; worker-src 'self'; manifest-src 'self'; "
        . "style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'";

    public static function secure(ResponseInterface $response, string $requestId): ResponseInterface
    {
        return $response
            ->withHeader('X-Request-Id', $requestId)
            ->withHeader('Content-Security-Policy', self::CSP)
            ->withHeader('X-Content-Type-Options', 'nosniff')
            ->withHeader('X-Frame-Options', 'DENY')
            ->withHeader('Referrer-Policy', 'same-origin');
    }

    public static function etagMatches(string $header, string $etag): bool
    {
        foreach (explode(',', $header) as $candidate) {
            $candidate = trim($candidate);
            if ($candidate === '*' || $candidate === $etag || $candidate === 'W/' . $etag) {
                return true;
            }
        }

        return false;
    }

    /** @param array<string,mixed> $body */
    public static function json(ResponseInterface $response, array $body, bool $head = false): ResponseInterface
    {
        if (!$head) {
            $response->getBody()->write(json_encode($body, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_INVALID_UTF8_SUBSTITUTE));
        }

        return $response->withHeader('Content-Type', 'application/json');
    }

    /** @param list<array{location:string,message:string}> $errors */
    public static function validation(
        ServerRequestInterface $request,
        ResponseInterface $response,
        array $errors,
    ): ResponseInterface {
        $response = $response->withStatus(422);
        $body = ['title' => $response->getReasonPhrase(), 'status' => 422, 'detail' => 'validation failed', 'errors' => $errors];
        $id = $request->getAttribute('requestId');
        if (is_string($id)) {
            $body['requestId'] = $id;
        }

        return self::json($response, $body)
            ->withHeader('Content-Type', 'application/problem+json')
            ->withHeader('Cache-Control', 'no-store');
    }

    public static function tooManyAttempts(ServerRequestInterface $request, int $retryAfter): ResponseInterface
    {
        return self::problem($request, 429, "too many attempts; try again in $retryAfter seconds")
            ->withHeader('Retry-After', (string) $retryAfter);
    }

    /** @param list<array{location:string,message:string}> $errors */
    public static function problem(
        ServerRequestInterface $request,
        int $status,
        string $detail,
        string $type = '',
        array $errors = [],
        string $reason = '',
    ): ResponseInterface {
        $response = new Response($status);
        $body = ['title' => $response->getReasonPhrase(), 'status' => $status, 'detail' => $detail];
        $requestId = $request->getAttribute('requestId');
        if (is_string($requestId)) {
            $body['requestId'] = $requestId;
        }
        if ($type !== '') {
            $body['type'] = $type;
        }
        if ($errors !== []) {
            $body['errors'] = $errors;
        }
        if ($reason !== '') {
            $body['reason'] = $reason;
        }

        return self::json($response, $body, $request->getMethod() === 'HEAD')
            ->withHeader('Content-Type', 'application/problem+json')
            ->withHeader('Cache-Control', 'no-store');
    }

    /** @param array{digest:string,contentType:string,data:string}|array{digest:string} $asset */
    public static function image(ResponseInterface $response, array $asset, bool $head = false): ResponseInterface
    {
        $response = $response
            ->withHeader('ETag', '"' . $asset['digest'] . '"')
            ->withHeader('Cache-Control', 'public, no-cache')
            ->withHeader('Content-Disposition', 'inline');
        if (!isset($asset['data'])) {
            return $response->withStatus(304);
        }
        if (!$head) {
            $response->getBody()->write($asset['data']);
        }

        return $response->withHeader('Content-Type', $asset['contentType']);
    }
}
