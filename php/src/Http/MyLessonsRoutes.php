<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use DateTimeImmutable;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\MyLessons;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;

final readonly class MyLessonsRoutes
{
    public function __construct(private MyLessons $lessons) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->get('/api/v1/my/lessons[/{view:history}]', $this->read(...));
    }

    /** @param array{view?:string} $args */
    private function read(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        try {
            $document = isset($args['view'])
                ? $this->lessons->history($principal, ...self::before(QueryParameters::read($request)['before'] ?? ''))
                : $this->lessons->upcoming($principal);

            return Responses::json($response, $document)->withHeader('Cache-Control', 'no-store');
        } catch (CatalogFailure $failure) {
            return CatalogAdminRoutes::failure($request, $failure);
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        }
    }

    /** @return array{?DateTimeImmutable,string} */
    private static function before(string $value): array
    {
        if ($value === '') {
            return [null, ''];
        }
        if (preg_match('/\A(\d{4})-(\d{2})-(\d{2})T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.(\d+))?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)\z/', $value, $parts) !== 1
            || !checkdate((int) $parts[2], (int) $parts[3], (int) $parts[1])) {
            throw new CatalogFailure(422, 'expected an RFC 3339 time', 'query.before');
        }

        $instant = new DateTimeImmutable($value);

        // PHP and the database store microseconds, but the API cursor can contain nanoseconds.
        $nanos = rtrim(substr($parts[4] ?? '', 6, 3), '0');
        if ($nanos === '' && $instant == new DateTimeImmutable('0001-01-01T00:00:00Z')) {
            return [null, ''];
        }

        return [$instant, $nanos];
    }
}
