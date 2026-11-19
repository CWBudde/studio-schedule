<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Monolog\Handler\TestHandler;
use Monolog\Logger;
use PHPUnit\Framework\TestCase;
use Psr\Http\Message\ResponseInterface;
use Slim\Psr7\Factory\ServerRequestFactory;
use StudioSchedule\Config;
use StudioSchedule\Http\Application;
use StudioSchedule\Service\Readiness;

final class ApplicationTest extends TestCase
{
    private const PREFIX = '/dance/schedule';
    private string $public;

    protected function setUp(): void
    {
        $this->public = sys_get_temp_dir() . '/studio-web-' . bin2hex(random_bytes(8));
        self::assertTrue(mkdir($this->public . '/assets', 0755, true));
        file_put_contents($this->public . '/index.html', '<html><base href="./"><div>app</div></html>');
        file_put_contents($this->public . '/assets/main.js', 'export {};');
        file_put_contents($this->public . '/sw.js', 'self.addEventListener("fetch", () => {});');
    }

    protected function tearDown(): void
    {
        unlink($this->public . '/assets/main.js');
        unlink($this->public . '/sw.js');
        unlink($this->public . '/index.html');
        rmdir($this->public . '/assets');
        rmdir($this->public);
    }

    public function testMountedProbesAndCacheHeaders(): void
    {
        $app = $this->app();
        $req = new ServerRequestFactory();
        $alive = $app->handle($req->createServerRequest('GET', 'https://example.test' . self::PREFIX . '/healthz'));
        self::assertSame(200, $alive->getStatusCode());
        self::assertSame('{"status":"ok"}', (string) $alive->getBody());
        self::assertSame('no-store', $alive->getHeaderLine('Cache-Control'));
        self::assertSame('nosniff', $alive->getHeaderLine('X-Content-Type-Options'));
        self::assertSame('max-age=31536000; includeSubDomains', $alive->getHeaderLine('Strict-Transport-Security'));

        $missing = $app->handle($req->createServerRequest('GET', 'https://example.test/healthz'));
        $this->assertProblem($missing, 404);
        $probe = $app->handle($req->createServerRequest('HEAD', 'https://example.test' . self::PREFIX . '/readyz'));
        self::assertSame(200, $probe->getStatusCode());
        self::assertSame('', (string) $probe->getBody());

        $health = $app->handle($req->createServerRequest('GET', 'https://example.test' . self::PREFIX . '/api/v1/health'));
        self::assertSame(200, $health->getStatusCode());
        self::assertSame('attachment; filename="api.json"', $health->getHeaderLine('Content-Disposition'));
    }

    public function testReadinessAndMaintenanceWhenSchemaUnavailable(): void
    {
        $app = $this->app(false);
        $req = new ServerRequestFactory();
        $base = 'https://example.test' . self::PREFIX;
        self::assertSame(200, $app->handle($req->createServerRequest('GET', $base . '/healthz'))->getStatusCode());
        $this->assertProblem($app->handle($req->createServerRequest('GET', $base . '/readyz')), 503);
        foreach (['/api/v1/settings/public', '/manifest.webmanifest'] as $path) {
            $maintenance = $app->handle($req->createServerRequest('GET', $base . $path));
            $this->assertProblem($maintenance, 503);
            self::assertStringContainsString('"type":"urn:studio-schedule:maintenance"', (string) $maintenance->getBody());
        }
    }

    public function testStaticFilesAndDeepLinks(): void
    {
        $app = $this->app();
        $req = new ServerRequestFactory();
        $base = 'https://example.test' . self::PREFIX;
        $redirect = $app->handle($req->createServerRequest('GET', $base . '?view=week'));
        self::assertSame(308, $redirect->getStatusCode());
        self::assertSame(self::PREFIX . '/?view=week', $redirect->getHeaderLine('Location'));

        $spa = $app->handle($req->createServerRequest('GET', $base . '/events/workshop'));
        self::assertSame(200, $spa->getStatusCode());
        self::assertStringContainsString('<base href="' . self::PREFIX . '/">', (string) $spa->getBody());
        self::assertSame('no-cache', $spa->getHeaderLine('Cache-Control'));
        $js = $app->handle($req->createServerRequest('GET', $base . '/assets/main.js'));
        self::assertSame('text/javascript; charset=utf-8', $js->getHeaderLine('Content-Type'));
        self::assertSame('public, max-age=31536000, immutable', $js->getHeaderLine('Cache-Control'));
        self::assertSame('export {};', (string) $js->getBody());
        $sw = $app->handle($req->createServerRequest('GET', $base . '/sw.js'));
        self::assertSame('no-cache', $sw->getHeaderLine('Cache-Control'));

        foreach (['/api/v1/unimplemented', '/assets/missing.js', '/ical/missing.ics', '/metrics'] as $path) {
            $this->assertProblem($app->handle($req->createServerRequest('GET', $base . $path)), 404);
        }
    }

    public function testCrossOriginRejectionAndRedactedRequestLogs(): void
    {
        $records = new TestHandler();
        $app = $this->app(true, (new Logger('test'))->pushHandler($records));
        $req = new ServerRequestFactory();
        $base = 'https://example.test' . self::PREFIX;
        $forged = $req->createServerRequest('POST', $base . '/api/v1/auth/logout')
            ->withHeader('Sec-Fetch-Site', 'cross-site');
        $this->assertProblem($app->handle($forged), 403);

        $secret = 'example-fixture-token';
        foreach (['event-invites', 'waitlist-invites'] as $route) {
            $missing = $req->createServerRequest('GET', $base . '/api/v1/' . $route . '/' . $secret . '?debug=private')
                ->withHeader('X-Request-Id', 'client-chosen-id');
            $answer = $app->handle($missing);
            $this->assertProblem($answer, 404);
            self::assertNotSame('client-chosen-id', $answer->getHeaderLine('X-Request-Id'));
            self::assertStringNotContainsString($secret, (string) $answer->getBody());
            self::assertStringContainsString('<redacted>', urldecode((string) $answer->getBody()));

            $contexts = array_map(static fn($record): array => $record->context, $records->getRecords());
            $log = json_encode($contexts, JSON_THROW_ON_ERROR);
            self::assertStringContainsString($answer->getHeaderLine('X-Request-Id'), $log);
            self::assertStringNotContainsString($secret, $log);
            self::assertStringNotContainsString('debug=private', $log);
        }
    }

    public function testRequestIDsComeFromTrustedPeers(): void
    {
        $req = new ServerRequestFactory();
        $app = $this->app(trusted: '192.0.2.0/24');
        foreach ([
            ['192.0.2.22', 'trusted-request-17', true],
            ['192.0.3.22', 'client-request-17', false],
            ['192.0.2.22', 'request id is invalid', false],
        ] as [$peer, $requested, $accepted]) {
            $health = $req->createServerRequest(
                'GET',
                'https://example.test' . self::PREFIX . '/healthz',
                ['REMOTE_ADDR' => $peer],
            )->withHeader('X-Request-Id', $requested);
            $message = $app->handle($health);
            self::assertSame(200, $message->getStatusCode());
            self::assertSame($accepted, $message->getHeaderLine('X-Request-Id') === $requested);
        }
    }

    public function testLegacyOriginHeadersAndUnsupportedMethods(): void
    {
        $app = $this->app();
        $req = new ServerRequestFactory();
        $base = 'https://example.test' . self::PREFIX;
        $post = $req->createServerRequest('POST', $base . '/healthz')->withHeader('Host', 'example.test');
        $this->assertProblem($app->handle($post->withHeader('Origin', 'https://attacker.example.test')), 403);
        $this->assertProblem($app->handle($post->withHeader('Origin', 'null')), 403);
        $this->assertProblem($app->handle($post->withHeader('Origin', 'https://example.test')), 405);
        $this->assertProblem($app->handle($post), 405);
        $this->assertProblem($app->handle($req->createServerRequest('POST', $base . '/events/workshop')), 405);

        foreach (['/index.php', '/assets/../index.php', '/assets/%00.js'] as $path) {
            $this->assertProblem($app->handle($req->createServerRequest('GET', $base . $path)), 404);
        }
        $asset = $app->handle($req->createServerRequest('HEAD', $base . '/assets/main.js'));
        self::assertSame(200, $asset->getStatusCode());
        self::assertSame('', (string) $asset->getBody());
    }

    public function testUncaughtErrorsAreLoggedAndReturnAnOpaqueProblem(): void
    {
        $logs = new TestHandler();
        $readiness = new class implements Readiness {
            public function ready(): bool
            {
                throw new \RuntimeException('private configuration fragment');
            }
        };
        $config = Config::load('/missing/studio-configuration', static fn(string $key): ?string => null);
        $app = Application::create($config, $this->public, $readiness, (new Logger('test'))->pushHandler($logs));
        $request = (new ServerRequestFactory())->createServerRequest('GET', 'http://example.test/readyz');
        $res = $app->handle($request);
        $this->assertProblem($res, 500);
        self::assertFalse($res->hasHeader('Strict-Transport-Security'));
        self::assertStringNotContainsString('private configuration fragment', (string) $res->getBody());
        self::assertCount(2, $logs->getRecords());

        $head = $app->handle($request->withMethod('HEAD'));
        self::assertSame(500, $head->getStatusCode());
        self::assertSame('', (string) $head->getBody());
    }

    public function testEncodedAPIRoutesUseTheSameMaintenanceAndCachePolicy(): void
    {
        $factory = new ServerRequestFactory();
        $base = 'https://example.test' . self::PREFIX;
        $slow = $this->app(false);
        $up = $this->app();
        foreach (['/%61pi/v1/health', '/api%2fv1/health'] as $path) {
            $request = $factory->createServerRequest('GET', $base . $path);
            $this->assertProblem($slow->handle($request), 503);
            $response = $up->handle($request);
            self::assertSame(200, $response->getStatusCode());
            self::assertSame('attachment; filename="api.json"', $response->getHeaderLine('Content-Disposition'));
        }
        $this->assertProblem($up->handle($factory->createServerRequest('GET', $base . '/api/v1/%ff')), 404);
    }

    private function app(
        bool $ready = true,
        ?Logger $logger = null,
        string $trusted = '',
    ): \Psr\Http\Server\RequestHandlerInterface {
        $config = Config::load('/missing/studio-configuration', static fn(string $key): ?string => match ($key) {
            'BASE_URL' => 'https://example.test' . self::PREFIX,
            'TRUSTED_PROXIES' => $trusted,
            default => null,
        });
        $state = new class ($ready) implements Readiness {
            public function __construct(private readonly bool $up) {}

            public function ready(): bool
            {
                return $this->up;
            }
        };

        return Application::create($config, $this->public, $state, $logger ?? new Logger('test'));
    }

    private function assertProblem(ResponseInterface $response, int $status): void
    {
        self::assertSame($status, $response->getStatusCode());
        self::assertSame('application/problem+json', $response->getHeaderLine('Content-Type'));
        self::assertMatchesRegularExpression('/^[a-f0-9-]{36}$/', $response->getHeaderLine('X-Request-Id'));
        self::assertStringContainsString($response->getHeaderLine('X-Request-Id'), (string) $response->getBody());
    }
}
