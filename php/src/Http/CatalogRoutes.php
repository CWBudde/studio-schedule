<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Clock\ClockInterface;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Slim\App;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\CatalogAdmin;
use StudioSchedule\Service\CatalogAdminPersistence;
use StudioSchedule\Service\CatalogPersistence;
use StudioSchedule\Service\CatalogService;

final readonly class CatalogRoutes
{
    private function __construct(private CatalogService $service, private ClockInterface $clock) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public static function register(App $app, CatalogPersistence&CatalogAdminPersistence $store, ClockInterface $clock): void
    {
        $routes = new self(new CatalogService($store), $clock);
        foreach (CatalogService::COLLECTIONS as $collection) {
            $app->get('/api/v1/' . $collection, $routes->handler($collection));
        }
        $app->get('/api/v1/terms/{termId}/classes', static fn(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
            => $routes->classes($request, $response, $args['termId'] ?? '', false));
        $app->get('/api/v1/admin/terms/{termId}/classes', static fn(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
            => $routes->classes($request, $response, $args['termId'] ?? '', true))->add(CatalogAdminRoutes::requireOffice(...));
        $app->get('/api/v1/admin/classes/{classId}/members', $routes->classMembers(...))->add(CatalogAdminRoutes::requireOffice(...));
        (new CatalogAdminRoutes(new CatalogAdmin($store)))->register($app);
    }

    /**
     * @param value-of<CatalogService::COLLECTIONS> $collection
     * @return callable(ServerRequestInterface, ResponseInterface):ResponseInterface
     */
    private function handler(string $collection): callable
    {
        return function (ServerRequestInterface $request, ResponseInterface $response) use ($collection): ResponseInterface {
            $location = $collection === 'rooms' ? self::location($request) : '';
            if ($location !== '' && !Id::valid($location)) {
                return self::invalidId($request, 'query.location', $location);
            }

            $items = $this->service->items($collection, $location === '' ? null : strtolower($location));

            return self::listResponse($request, $response, $items);
        };
    }

    private function classes(ServerRequestInterface $request, ResponseInterface $response, mixed $term, bool $withRetired): ResponseInterface
    {
        if ($request->getMethod() === 'HEAD') {
            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', $withRetired ? 'GET' : 'GET, POST');
        }
        if (!is_string($term) || !Id::valid($term)) {
            return self::invalidId($request, 'path.termId', is_string($term) ? $term : '');
        }

        return self::listResponse($request, $response, $this->service->classes(strtolower($term), $withRetired));
    }

    /** @param array<string,mixed> $args */
    private function classMembers(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $class = $args['classId'] ?? '';
        if (!is_string($class) || !Id::valid($class)) {
            return self::invalidId($request, 'path.classId', is_string($class) ? $class : '');
        }

        $items = $this->service->classMembers(strtolower($class), $this->clock->now());

        return self::listResponse($request, $response, $items)->withHeader('Cache-Control', 'no-store');
    }

    /** @param list<array<string,mixed>>|null $items */
    private static function listResponse(ServerRequestInterface $request, ResponseInterface $response, ?array $items): ResponseInterface
    {
        return $items === null
            ? Responses::problem($request, 404, 'resource not found')
            : Responses::json($response, ['items' => $items]);
    }

    private static function location(ServerRequestInterface $request): string
    {
        // Match Huma's first-value lookup on the wire query, before PSR-7 normalizes escapes.
        $query = $request->getServerParams()['QUERY_STRING'] ?? null;
        if (!is_string($query)) {
            $query = $request->getUri()->getQuery();
        }
        $length = strlen($query);
        for ($offset = 0; $offset < $length;) {
            $separator = strpos($query, '&', $offset);
            $assignment = strpos($query, '=', $offset);
            $end = $assignment === false ? ($separator === false ? $length : $separator) : $assignment;
            if (urldecode(substr($query, $offset, $end - $offset)) === 'location') {
                if ($assignment === false) {
                    return 'true';
                }

                $value = substr($query, $assignment + 1, ($separator === false ? $length : $separator) - $assignment - 1);

                return preg_match('/%(?![0-9a-f]{2})/i', $value) === 1 ? '' : urldecode($value);
            }
            if ($separator === false) {
                break;
            }
            $offset = $separator + 1;
        }

        return '';
    }

    private static function invalidId(ServerRequestInterface $request, string $location, string $value): ResponseInterface
    {
        $quoted = json_encode($value, JSON_THROW_ON_ERROR | JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE);

        return Responses::problem($request, 422, 'validation failed', errors: [
            ['location' => $location, 'message' => 'invalid value: validation failed: invalid ID ' . $quoted],
        ]);
    }
}
