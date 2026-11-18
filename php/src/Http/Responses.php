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

    /** @param array<string,mixed> $body */
    public static function json(ResponseInterface $response, array $body, bool $head = false): ResponseInterface
    {
        if (!$head) {
            $response->getBody()->write(json_encode($body, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_INVALID_UTF8_SUBSTITUTE));
        }

        return $response->withHeader('Content-Type', 'application/json');
    }

    public static function problem(
        ServerRequestInterface $request,
        int $status,
        string $detail,
        string $type = '',
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

        return self::json($response, $body, $request->getMethod() === 'HEAD')
            ->withHeader('Content-Type', 'application/problem+json')
            ->withHeader('Cache-Control', 'no-store');
    }
}
