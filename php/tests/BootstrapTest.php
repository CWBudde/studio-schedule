<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use PHPUnit\Framework\TestCase;
use Slim\Psr7\Factory\ServerRequestFactory;
use StudioSchedule\Bootstrap;

final class BootstrapTest extends TestCase
{
    public function testBootstrapBuildsTheMountedApplicationFromItsEnvironment(): void
    {
        $factory = new ServerRequestFactory();
        $values = [
            'BASE_URL' => 'http://example.test/studio',
            'DATABASE_URL' => 'postgresql://localhost/studio',
            'LOG_FORMAT' => 'json',
            'LOG_LEVEL' => 'warn',
        ];
        $response = Bootstrap::handle(
            static fn() => $factory->createServerRequest('GET', 'http://example.test/studio/healthz'),
            '/missing/studio-app',
            '/missing/studio-web',
            '/missing/studio-migrations',
            static fn(string $key): ?string => $values[$key] ?? null,
        );
        self::assertSame(200, $response->getStatusCode());
        self::assertSame('{"status":"ok"}', (string) $response->getBody());
    }

    public function testBadRequestFactoryAndConfigurationReturnOpaqueProblems(): void
    {
        $factory = new ServerRequestFactory();
        foreach ([
            static fn() => $factory->createServerRequest('GET', '/'),
            static function (): never {
                throw new \RuntimeException('fixture secret');
            },
        ] as $createRequest) {
            $response = Bootstrap::handle(
                $createRequest,
                '/missing/studio-app',
                '/missing/studio-web',
                '/missing/studio-migrations',
                static fn(string $key): ?string => $key === 'BASE_URL' ? 'fixture secret' : null,
            );
            self::assertSame(500, $response->getStatusCode());
            self::assertSame('nosniff', $response->getHeaderLine('X-Content-Type-Options'));
            $id = $response->getHeaderLine('X-Request-Id');
            self::assertNotSame('', $id);
            self::assertStringContainsString($id, (string) $response->getBody());
            self::assertStringNotContainsString('fixture secret', (string) $response->getBody());
        }
    }
}
