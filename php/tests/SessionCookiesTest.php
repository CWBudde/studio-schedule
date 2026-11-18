<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Config;
use StudioSchedule\Http\SessionCookies;

final class SessionCookiesTest extends TestCase
{
    public function testCookieAttributesMatchTheGoServerAtBothMounts(): void
    {
        $expiry = new DateTimeImmutable('2026-12-04 13:30:00+01:00');
        foreach ([
            ['https://school.example', '__Host-studio_session', '/', '; Secure'],
            ['https://school.example/dance/schedule', 'studio_session', '/dance/schedule', '; Secure'],
            ['http://localhost:8080', 'studio_session', '/', ''],
        ] as [$base, $name, $path, $secure]) {
            $config = Config::load('/missing/.env', static fn(string $key): ?string => $key === 'BASE_URL' ? $base : null);
            $cookies = new SessionCookies($config);
            self::assertSame(
                "$name=synthetic-token; Path=$path; Expires=Fri, 04 Dec 2026 12:30:00 GMT; HttpOnly$secure; SameSite=Lax",
                $cookies->issue('synthetic-token', $expiry),
            );
            self::assertSame(
                "$name=; Path=$path; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Max-Age=0; HttpOnly$secure; SameSite=Lax",
                $cookies->expire($name),
            );
        }
    }
}
