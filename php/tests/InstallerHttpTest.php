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

    public function testFailedDatabaseConnectionDoesNotExposeCredentials(): void
    {
        $response = $this->request(
            'GET',
            '/dance/setup',
            ['SETUP_TOKEN' => 'fixture secret', 'DATABASE_URL' => 'postgresql://studio:copy-fixture-password@127.0.0.1:1/fixture'],
            ['token' => 'fixture secret'],
        );

        self::assertSame(503, $response->getStatusCode());
        self::assertStringContainsString('The database connection failed.', (string) $response->getBody());
        self::assertStringNotContainsString('copy-fixture-password', (string) $response->getBody());
    }

    public function testRestrictedLogPathDoesNotRenderPhpWarnings(): void
    {
        [$status, $body] = $this->installerProcess([
            '-d', 'open_basedir=' . dirname(__DIR__, 2) . PATH_SEPARATOR . sys_get_temp_dir(),
            '-d', 'error_log=/var/studio-outside-base/error.log',
            '-d', 'display_errors=1',
        ]);
        self::assertSame('503', $status);
        self::assertStringContainsString('configured error_log directory must be writable', $body);
        self::assertStringNotContainsString('studio-outside-base', $body);
    }

    public function testAuthenticatedInstallerChecksTheLogDirectoryBeforeConnecting(): void
    {
        $previous = ini_get('error_log');
        self::assertIsString($previous);
        $log = sys_get_temp_dir() . '/studio-missing-' . bin2hex(random_bytes(6)) . '/private.log';
        self::assertNotFalse(ini_set('error_log', $log));
        try {
            $response = $this->request(
                'POST',
                '/dance/setup',
                ['SETUP_TOKEN' => 'fixture secret', 'DATABASE_URL' => 'postgresql://studio:private@127.0.0.1:1/fixture'],
                body: ['token' => 'fixture secret', 'action' => 'install'],
            );
            self::assertSame(503, $response->getStatusCode());
            self::assertStringContainsString('configured error_log directory must be writable', (string) $response->getBody());
            self::assertStringNotContainsString('private', (string) $response->getBody());
        } finally {
            ini_set('error_log', $previous);
        }
    }

    public function testInstallerNamesMissingReleaseExtensions(): void
    {
        // GD and the PostgreSQL driver are dynamically loaded in the PHP test image.
        [$status, $body] = $this->installerProcess(['-n']);
        self::assertSame('503', $status);
        self::assertStringContainsString('gd', $body);
        self::assertStringContainsString('pdo_pgsql', $body);
        self::assertStringContainsString('PHP extensions', $body);
        self::assertStringNotContainsString('copy-fixture-password', $body);
    }

    public function testInstallerChecksUsableArgon2idHashing(): void
    {
        [$status, $body] = $this->installerProcess([
            '-d', 'disable_functions=password_algos,password_hash,password_verify,sodium_crypto_pwhash,sodium_crypto_pwhash_str_verify',
        ]);
        self::assertSame('503', $status);
        self::assertStringContainsString('Argon2id password hashing and verification', $body);
        self::assertStringNotContainsString('copy-fixture-password', $body);
    }

    /**
     * @param list<string> $iniFlags
     * @return array{string,string}
     */
    private function installerProcess(array $iniFlags): array
    {
        $script = <<<'PHP'
            putenv('BASE_URL=https://example.test/dance');
            putenv('SETUP_TOKEN=fixture-token');
            putenv('DATABASE_URL=postgresql://fixture:copy-fixture-password@127.0.0.1:1/fixture');
            $_SERVER['REQUEST_METHOD'] = 'GET';
            $_SERVER['REQUEST_URI'] = '/dance/setup?token=fixture-token';
            $_GET['token'] = 'fixture-token';
            register_shutdown_function(static fn() => fwrite(STDERR, 'Status ' . http_response_code()));
            require 'public/index.php';
            PHP;
        $pipes = [];
        $child = proc_open(
            [PHP_BINARY, ...$iniFlags, '-r', $script],
            [1 => ['pipe', 'w'], 2 => ['pipe', 'w']],
            $pipes,
            dirname(__DIR__),
        );
        self::assertIsResource($child);
        $body = stream_get_contents($pipes[1]);
        $details = stream_get_contents($pipes[2]);
        fclose($pipes[1]);
        fclose($pipes[2]);
        self::assertSame(0, proc_close($child), (string) $details);
        self::assertSame(1, preg_match('/Status (\d+)$/D', (string) $details, $parts), (string) $details);

        return [$parts[1] ?? '', (string) $body];
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
