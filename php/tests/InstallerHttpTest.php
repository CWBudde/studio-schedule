<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use PHPUnit\Framework\TestCase;
use Psr\Http\Message\ResponseInterface;
use Slim\Psr7\Factory\ServerRequestFactory;
use StudioSchedule\Bootstrap;

final class InstallerHttpTest extends TestCase
{
    public function testInstallerIsHiddenWithoutItsToken(): void
    {
        $this->expectHidden($this->request('GET', '/dance/setup'));
        $this->expectHidden($this->request('GET', '/dance/setup', ['SETUP_TOKEN' => 'fixture secret']));
        $this->expectHidden($this->request(
            'GET',
            '/dance/setup',
            ['SETUP_TOKEN' => 'fixture secret'],
            ['token' => 'wrong token'],
        ));
        $this->expectHidden($this->request('POST', '/dance/setup', ['SETUP_TOKEN' => 'fixture secret'], [], [
            'token' => ['fixture secret'],
        ]));
    }

    public function testAuthenticatedInstallerCanReportBrokenDatabaseConfiguration(): void
    {
        $response = $this->request(
            'GET',
            '/dance/setup',
            ['SETUP_TOKEN' => 'fixture secret', 'DATABASE_URL' => 'invalid private connection details'],
            ['token' => 'fixture secret'],
        );

        self::assertSame(503, $response->getStatusCode());
        self::assertSame('no-store', $response->getHeaderLine('Cache-Control'));
        self::assertSame('no-referrer', $response->getHeaderLine('Referrer-Policy'));
        self::assertStringContainsString('DATABASE_URL', (string) $response->getBody());
        self::assertStringNotContainsString('invalid private connection details', (string) $response->getBody());
    }

    /** @param array<string,string> $environment
     *  @param array<string,mixed> $query
     *  @param array<string,mixed> $body
     */
    private function request(
        string $method,
        string $path,
        array $environment = [],
        array $query = [],
        array $body = [],
    ): ResponseInterface {
        $factory = new ServerRequestFactory();
        $request = $factory->createServerRequest($method, 'https://school.example.test' . $path)
            ->withQueryParams($query)
            ->withParsedBody($body);
        $values = ['BASE_URL' => 'https://school.example.test/dance', 'LOG_LEVEL' => 'error'] + $environment;

        return Bootstrap::handle(
            static fn() => $request,
            '/missing/studio-app',
            '/missing/studio-web',
            '/missing/studio-migrations',
            static fn(string $key): ?string => $values[$key] ?? null,
        );
    }

    private function expectHidden(ResponseInterface $response): void
    {
        self::assertSame(404, $response->getStatusCode());
        self::assertStringNotContainsString('fixture secret', (string) $response->getBody());
    }
}
