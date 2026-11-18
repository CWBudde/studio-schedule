<?php

declare(strict_types=1);

namespace StudioSchedule\Tests\Conformance;

use DateTimeImmutable;
use DateTimeZone;
use InvalidArgumentException;
use JsonException;
use Psr\Clock\ClockInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use RuntimeException;
use Slim\Psr7\Response;
use StudioSchedule\Bootstrap;
use StudioSchedule\Config;
use StudioSchedule\Domain\Date;
use StudioSchedule\Http\Responses;
use StudioSchedule\Store\Database;
use Throwable;

final readonly class FixtureServer
{
    public function __construct(
        private Config $config,
        #[\SensitiveParameter]
        private string $token,
        private string $statePath,
        private string $appDirectory,
        private string $publicDirectory,
        private string $postgresMigrations,
        private FixtureStore $store,
    ) {}

    public static function fromEnvironment(string $appDirectory, string $root): self
    {
        $settings = $appDirectory . '/config/.env';
        $get = Config::lookup($settings);
        $config = Config::load($settings);
        $token = $get('TEST_CONTROL_TOKEN');
        $host = parse_url($config->baseURL, PHP_URL_HOST);
        if ($get('TEST_CONTROL') !== 'true' || strlen($token) < 32 || $config->secure
            || !in_array($host, ['127.0.0.1', '[::1]', 'localhost'], true)
        ) {
            throw new RuntimeException('The conformance fixture requires TEST_CONTROL, a bearer token and a loopback HTTP BASE_URL.');
        }

        $statePath = $get('STUDIO_CONFORMANCE_STATE');
        $goBinary = $get('STUDIO_CONFORMANCE_GO');
        if (!str_starts_with($statePath, '/') || !is_file($statePath) || !is_writable($statePath)
            || !str_starts_with($goBinary, '/') || !is_file($goBinary) || !is_executable($goBinary)
        ) {
            throw new RuntimeException('The conformance fixture needs its state file and reference Go executable.');
        }

        // The launcher provisions dedicated databases with this exact name.
        $fixtureDatabase = 'studio_conformance_' . substr(hash('sha256', $token), 0, 16);
        $target = $config->databaseURL;
        $seed = Database::driverForURL($target) === 'pdo_mysql' ? $get('STUDIO_CONFORMANCE_SEED_URL') : null;
        self::requireLoopback($target);
        if ($seed !== null) {
            self::requireLoopback($seed);
        }

        $postgresMigrations = $root . '/internal/store/postgres/migrations';
        $store = new FixtureStore(
            $target,
            $seed,
            $fixtureDatabase,
            $goBinary,
            $postgresMigrations,
            $appDirectory . '/migrations/mysql',
        );

        return new self($config, $token, $statePath, $appDirectory, $root . '/web/dist', $postgresMigrations, $store);
    }

    private static function requireLoopback(#[\SensitiveParameter] string $url): void
    {
        if (!in_array(parse_url($url, PHP_URL_HOST), ['127.0.0.1', '[::1]', 'localhost'], true)) {
            throw new RuntimeException('The conformance fixture requires a disposable loopback database.');
        }
    }

    public function handle(ServerRequestInterface $request): ResponseInterface
    {
        if (!in_array($request->getServerParams()['REMOTE_ADDR'] ?? '', ['127.0.0.1', '::1'], true)) {
            return self::problem($request, 404, 'not found');
        }

        $path = rawurldecode($request->getUri()->getPath());
        $prefix = $this->config->basePath . '/api/test';
        $control = $path === $prefix || str_starts_with($path, $prefix . '/');
        if ($control && !hash_equals('Bearer ' . $this->token, $request->getHeaderLine('Authorization'))) {
            return self::problem($request, 401, 'Invalid fixture credential.')->withHeader('WWW-Authenticate', 'Bearer');
        }

        $file = fopen($this->statePath, 'r+');
        if ($file === false) {
            throw new RuntimeException('Could not open conformance fixture state.');
        }
        try {
            if (!flock($file, LOCK_EX)) {
                throw new RuntimeException('Could not lock conformance fixture state.');
            }
            $state = self::readState($file);
            if ($control) {
                return $this->control($request, substr($path, strlen($prefix)), $file, $state)
                    ->withHeader('Cache-Control', 'no-store')->withHeader('X-Content-Type-Options', 'nosniff');
            }
            if (!$state['ready'] && $path !== $this->config->basePath . '/healthz') {
                return self::problem($request, 503, 'The conformance fixture requires a successful reset.');
            }

            $clock = $state['now'] === null ? null : new class (new DateTimeImmutable($state['now'])) implements ClockInterface {
                public function __construct(private readonly DateTimeImmutable $now) {}

                public function now(): DateTimeImmutable
                {
                    return $this->now;
                }
            };

            return Bootstrap::handle(
                static fn() => $request,
                $this->appDirectory,
                $this->publicDirectory,
                $this->postgresMigrations,
                clock: $clock,
            );
        } catch (Throwable $error) {
            error_log('PHP conformance fixture failure: ' . $error::class);

            return self::problem($request, 500, 'Conformance fixture operation failed.');
        } finally {
            fclose($file);
        }
    }

    /**
     * @param resource $file
     * @param array{ready:bool,now:?string} $state
     */
    private function control(ServerRequestInterface $request, string $path, $file, array $state): ResponseInterface
    {
        $method = match ($path) {
            '/reset', '/seed' => 'POST',
            '/clock' => 'PUT',
            '/outbox' => 'GET',
            default => null,
        };
        if ($method === null) {
            return self::problem($request, 404, 'Unknown fixture operation.');
        }
        if ($request->getMethod() !== $method) {
            return self::problem($request, 405, 'Unexpected fixture request method.')->withHeader('Allow', $method);
        }

        if ($path === '/reset') {
            $state['ready'] = false;
            self::writeState($file, $state);
            $this->store->reset();
            $state['ready'] = true;
            self::writeState($file, $state);

            return new Response(204);
        }
        if ($path === '/clock') {
            try {
                $state['now'] = self::instant(self::fields($request, ['now'])['now']);
            } catch (InvalidArgumentException|JsonException) {
                return self::problem($request, 400, 'Expected an RFC 3339 fixture clock.');
            }
            self::writeState($file, $state);

            return new Response(204);
        }
        if (!$state['ready']) {
            return self::problem($request, 503, 'The conformance fixture requires a successful reset.');
        }
        if ($path === '/outbox') {
            return Responses::json(new Response(), ['mails' => $this->store->outbox()]);
        }

        try {
            ['anchor' => $anchor, 'school' => $school] = self::fields($request, ['anchor', 'school']);
            $date = Date::parse($anchor);
            if (!in_array($school, ['programme', 'werkstatt'], true)) {
                throw new InvalidArgumentException('Unknown demo school.');
            }
        } catch (InvalidArgumentException|JsonException) {
            return self::problem($request, 400, 'Expected a demo school and a YYYY-MM-DD anchor.');
        }

        return $this->store->seed($date, $school) ? new Response(204) : self::problem($request, 409, 'The fixture is already seeded.');
    }

    private static function instant(string $value): string
    {
        if (preg_match('/\A(\d{4}-\d{2}-\d{2})T((?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d)(?:\.(\d+))?(Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)\z/', $value, $match) !== 1) {
            throw new InvalidArgumentException('Invalid fixture clock.');
        }
        Date::parse($match[1]);
        $fraction = str_pad(substr($match[3], 0, 6), 6, '0');
        $now = new DateTimeImmutable("$match[1]T$match[2].$fraction$match[4]");
        $utc = $now->setTimezone(new DateTimeZone('UTC'))->format('Y-m-d\TH:i:s.u\Z');
        if (strcmp($utc, '0001-01-01T00:00:00.000000Z') <= 0 || strlen($utc) !== 27) {
            throw new InvalidArgumentException('Fixture clock outside the supported range.');
        }

        return $utc;
    }

    /**
     * @template Key of string
     * @param non-empty-list<Key> $expected
     * @return array<Key,string>
     */
    private static function fields(ServerRequestInterface $request, array $expected): array
    {
        $stream = $request->getBody();
        $body = '';
        // php://input can report EOF only after the read that crosses the end.
        while (strlen($body) < 4097) {
            $chunk = $stream->read(4097 - strlen($body));
            if ($chunk === '') {
                break;
            }
            $body .= $chunk;
        }
        if (strlen($body) > 4096) {
            throw new InvalidArgumentException('Oversized fixture request.');
        }
        $object = json_decode($body, true, 8, JSON_THROW_ON_ERROR);
        if (!is_array($object) || count($object) !== count($expected)) {
            throw new InvalidArgumentException('Unexpected fixture fields.');
        }
        $fields = [];
        foreach ($expected as $name) {
            $value = $object[$name] ?? null;
            if (!is_string($value)) {
                throw new InvalidArgumentException('Unexpected fixture field type.');
            }
            $fields[$name] = $value;
        }

        return $fields;
    }

    /** @param resource $file
     *  @return array{ready:bool,now:?string}
     */
    private static function readState($file): array
    {
        $contents = stream_get_contents($file);
        if ($contents === '') {
            return ['ready' => false, 'now' => null];
        }
        if ($contents === false) {
            throw new RuntimeException('Could not read conformance fixture state.');
        }
        $value = json_decode($contents, true, 8, JSON_THROW_ON_ERROR);
        if (!is_array($value) || !is_bool($value['ready'] ?? null)) {
            throw new RuntimeException('Invalid conformance fixture state.');
        }
        $now = $value['now'] ?? null;
        if ($now !== null && !is_string($now)) {
            throw new RuntimeException('Invalid conformance fixture clock state.');
        }

        return ['ready' => $value['ready'], 'now' => $now];
    }

    /** @param resource $file
     *  @param array{ready:bool,now:?string} $state
     */
    private static function writeState($file, array $state): void
    {
        $value = json_encode($state, JSON_THROW_ON_ERROR);
        if (!rewind($file) || !ftruncate($file, 0) || fwrite($file, $value) !== strlen($value) || !fflush($file)) {
            throw new RuntimeException('Could not save conformance fixture state.');
        }
    }

    private static function problem(ServerRequestInterface $request, int $status, string $message): ResponseInterface
    {
        return Responses::problem($request, $status, $message)->withHeader('X-Content-Type-Options', 'nosniff');
    }
}
