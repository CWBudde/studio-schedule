<?php

declare(strict_types=1);

namespace StudioSchedule;

use InvalidArgumentException;
use RuntimeException;
use StudioSchedule\Http\TrustedProxies;
use Symfony\Component\Dotenv\Dotenv;

final readonly class Config
{
    private function __construct(
        public string $baseURL,
        public string $basePath,
        public bool $secure,
        public bool $hstsSubdomains,
        #[\SensitiveParameter]
        public string $databaseURL,
        #[\SensitiveParameter]
        public string $metricsToken,
        public string $logFormat,
        public string $logLevel,
        public TrustedProxies $trustedProxies,
    ) {}

    /** @param (callable(string): ?string)|null $environment */
    public static function load(string $file, ?callable $environment = null): self
    {
        $fileValues = self::readFile($file);
        $environment ??= static function (string $key): ?string {
            $value = getenv($key);
            return $value === false ? null : $value;
        };

        $get = static fn(string $key): string => trim($environment($key) ?? $fileValues[$key] ?? '');

        [$baseURL, $basePath, $secure] = self::base($get('BASE_URL'));
        $logFormat = $get('LOG_FORMAT') ?: 'text';
        if (!in_array($logFormat, ['text', 'json'], true)) {
            throw new InvalidArgumentException('LOG_FORMAT must be text or json');
        }

        $logLevel = $get('LOG_LEVEL') ?: 'info';
        if (!in_array($logLevel, ['debug', 'info', 'warn', 'error'], true)) {
            throw new InvalidArgumentException('LOG_LEVEL must be debug, info, warn or error');
        }

        return new self(
            $baseURL,
            $basePath,
            $secure,
            self::boolean('HSTS_INCLUDE_SUBDOMAINS', $get('HSTS_INCLUDE_SUBDOMAINS'), true),
            $get('DATABASE_URL'),
            $get('METRICS_TOKEN'),
            $logFormat,
            $logLevel,
            TrustedProxies::parse($get('TRUSTED_PROXIES')),
        );
    }

    public function cookieName(): string
    {
        return $this->secure && $this->basePath === '' ? '__Host-studio_session' : 'studio_session';
    }

    public function cookiePath(): string
    {
        return $this->basePath === '' ? '/' : $this->basePath;
    }

    /** @return array<string,string> */
    private static function readFile(string $file): array
    {
        if (!is_file($file)) {
            return [];
        }
        $content = file_get_contents($file);
        if ($content === false) {
            throw new RuntimeException('could not read the app configuration');
        }

        /** @var array<string,string> $values Dotenv parses variable names and their string values. */
        $values = (new Dotenv())->parse($content, $file);
        return $values;
    }

    /** @return array{string,string,bool} */
    private static function base(string $raw): array
    {
        if ($raw === '') {
            return ['', '', false];
        }

        $parts = parse_url($raw);
        $scheme = strtolower($parts['scheme'] ?? '');
        if ($parts === false || ($parts['host'] ?? '') === '' || !in_array($scheme, ['https', 'http'], true)
            || isset($parts['user']) || isset($parts['pass']) || isset($parts['query']) || isset($parts['fragment'])) {
            throw new InvalidArgumentException('BASE_URL must be an HTTP(S) URL without credentials, query or fragment');
        }

        $path = $parts['path'] ?? '';
        $path = str_ends_with($path, '/') ? substr($path, 0, -1) : $path;
        if (preg_match('#^(?:/[a-zA-Z0-9._~-]+)*$#D', $path) !== 1
            || in_array('.', explode('/', $path), true) || in_array('..', explode('/', $path), true)) {
            throw new InvalidArgumentException('BASE_URL must have a clean path using unreserved URL characters');
        }

        return [str_ends_with($raw, '/') ? substr($raw, 0, -1) : $raw, $path, $scheme === 'https'];
    }

    private static function boolean(string $key, string $value, bool $default): bool
    {
        return match ($value) {
            '' => $default,
            '1', 't', 'T', 'true', 'TRUE', 'True' => true,
            '0', 'f', 'F', 'false', 'FALSE', 'False' => false,
            default => throw new InvalidArgumentException($key . ' must be true or false'),
        };
    }
}
