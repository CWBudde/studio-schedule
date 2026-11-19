<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use StudioSchedule\Store\RateLimitReservation;
use StudioSchedule\Store\RateLimitStore;

final readonly class AuthMailLimit
{
    private const string BUCKET = 'auth-mail-ip';
    private const int LIMIT = 5;
    private const int WINDOW_SECONDS = 3600;

    public function __construct(private TrustedProxies $trustedProxies, private RateLimitStore $limits) {}

    public function retryAfter(#[SensitiveParameter] ServerRequestInterface $request): int
    {
        return $this->limits->allow(self::BUCKET, $this->address($request), self::LIMIT, self::WINDOW_SECONDS);
    }

    public function reserve(#[SensitiveParameter] ServerRequestInterface $request): RateLimitReservation|int
    {
        return $this->limits->reserve(self::BUCKET, $this->address($request), self::LIMIT, self::WINDOW_SECONDS);
    }

    private function address(#[SensitiveParameter] ServerRequestInterface $request): string
    {
        $peer = $request->getServerParams()['REMOTE_ADDR'] ?? '';

        return $this->trustedProxies->clientIP(is_string($peer) ? $peer : '', $request->getHeaderLine('X-Forwarded-For'));
    }
}
