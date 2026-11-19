<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Slim\App;
use StudioSchedule\Service\CatalogAdmin;
use StudioSchedule\Service\CatalogAdminPersistence;
use StudioSchedule\Service\CatalogPersistence;
use StudioSchedule\Service\CatalogService;

final readonly class CatalogRoutes
{
    private function __construct(private CatalogService $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public static function register(App $app, CatalogPersistence&CatalogAdminPersistence $store): void
    {
        $routes = new self(new CatalogService($store));
        foreach (CatalogService::COLLECTIONS as $collection) {
            $app->get('/api/v1/' . $collection, $routes->handler($collection));
        }
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
            if ($location !== '' && !self::validId($location)) {
                return self::invalidLocation($request, $response, $location);
            }

            $items = $this->service->items($collection, $location === '' ? null : strtolower($location));

            return $items === null
                ? Responses::problem($request, 404, 'resource not found')
                : Responses::json($response, ['items' => $items]);
        };
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

    private static function validId(string $value): bool
    {
        return $value !== '00000000-0000-0000-0000-000000000000'
            && preg_match('/\A[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\z/i', $value) === 1;
    }

    private static function invalidLocation(ServerRequestInterface $request, ResponseInterface $response, string $location): ResponseInterface
    {
        $response = $response->withStatus(422);
        $quoted = json_encode($location, JSON_THROW_ON_ERROR | JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE);
        $body = [
            'title' => $response->getReasonPhrase(),
            'status' => 422,
            'detail' => 'validation failed',
            'errors' => [['location' => 'query.location', 'message' => 'invalid value: validation failed: invalid ID ' . $quoted]],
        ];
        $requestId = $request->getAttribute('requestId');
        if (is_string($requestId)) {
            $body['requestId'] = $requestId;
        }

        return Responses::json($response, $body)
            ->withHeader('Content-Type', 'application/problem+json')
            ->withHeader('Cache-Control', 'no-store');
    }
}
