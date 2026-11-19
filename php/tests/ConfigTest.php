<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use InvalidArgumentException;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Config;

final class ConfigTest extends TestCase
{
    public function testApplicationURLUsesTheMountOrTheConfiguredLocalPort(): void
    {
        self::assertSame('https://school.example.test/dance', Config::applicationURL('https://school.example.test/dance', ':9110'));
        self::assertSame('http://localhost:9110', Config::applicationURL('', ':9110'));
        self::assertSame('http://localhost:8080', Config::applicationURL('', ''));
    }

    public function testEnvironmentWinsOverFileIncludingAnExplicitEmptyValue(): void
    {
        $file = tempnam(sys_get_temp_dir(), 'studio-env-');
        self::assertNotFalse($file);
        file_put_contents($file, <<<'ENV'
            BASE_URL='https://fallback.example.test/'
            DATABASE_URL='postgresql://studio@localhost/test'
            LOG_FORMAT='json'
            ENV);
        try {
            $values = ['BASE_URL' => 'https://example.test/dance/schedule/', 'LOG_FORMAT' => ''];
            $config = Config::load($file, static fn(string $key): ?string => $values[$key] ?? null);
            self::assertSame('https://example.test/dance/schedule', $config->baseURL);
            self::assertSame('/dance/schedule', $config->basePath);
            self::assertSame('postgresql://studio@localhost/test', $config->databaseURL);
            self::assertSame('text', $config->logFormat);
            self::assertSame('studio_session', $config->cookieName());
            self::assertSame('/dance/schedule', $config->cookiePath());
            self::assertTrue($config->secure);
        } finally {
            unlink($file);
        }
    }

    public function testRootCookieAndDefaults(): void
    {
        $config = Config::load('/missing/studio-configuration', static fn(string $key): ?string
            => $key === 'BASE_URL' ? 'https://example.test/' : null);
        self::assertSame('', $config->basePath);
        self::assertSame('/', $config->cookiePath());
        self::assertTrue($config->secure);
        self::assertSame('__Host-studio_session', $config->cookieName());
        self::assertSame('info', $config->logLevel);
        self::assertTrue($config->hstsSubdomains);

        $default = Config::load('/missing/studio-configuration', static fn(string $key): ?string => null);
        self::assertFalse($default->secure);
        self::assertSame('studio_session', $default->cookieName());
        self::assertSame('', $default->databaseURL);
        self::assertSame('', $default->metricsToken);
    }

    public function testProcessEnvironmentAndBooleanOverrides(): void
    {
        $before = getenv('HSTS_INCLUDE_SUBDOMAINS');
        try {
            putenv('HSTS_INCLUDE_SUBDOMAINS=false');
            $config = Config::load('/missing/studio-configuration');
            self::assertFalse($config->hstsSubdomains);
        } finally {
            putenv($before === false ? 'HSTS_INCLUDE_SUBDOMAINS' : 'HSTS_INCLUDE_SUBDOMAINS=' . $before);
        }
    }

    /** @return list<array{string,string}> */
    public static function invalidSettings(): array
    {
        return [['LOG_FORMAT', 'xml'], ['LOG_LEVEL', 'loud'], ['HSTS_INCLUDE_SUBDOMAINS', 'perhaps']];
    }

    #[DataProvider('invalidSettings')]
    public function testInvalidSettingsAreNamed(string $name, string $value): void
    {
        $this->expectException(InvalidArgumentException::class);
        $this->expectExceptionMessage($name);
        Config::load('/missing/studio-configuration', static fn(string $key): ?string => $key === $name ? $value : null);
    }

    /** @return array<string,array{string}> */
    public static function invalidBaseURLs(): array
    {
        return [
            'relative' => ['/schedule'],
            'credentials' => ['https://user:password@example.test/schedule'],
            'scheme' => ['ftp://example.test/schedule'],
            'query' => ['https://example.test/schedule?view=week'],
            'fragment' => ['https://example.test/schedule#'],
            'encoded' => ['https://example.test/%73chedule'],
            'empty segment' => ['https://example.test//schedule'],
            'dot segment' => ['https://example.test/a/../schedule'],
        ];
    }

    #[DataProvider('invalidBaseURLs')]
    public function testRejectsNonCanonicalBaseURL(string $url): void
    {
        $this->expectException(InvalidArgumentException::class);
        $this->expectExceptionMessage('BASE_URL');
        Config::load('/missing/studio-configuration', static fn(string $key): ?string => $key === 'BASE_URL' ? $url : null);
    }
}
