<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Clock\ClockInterface;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Slim\App;
use StudioSchedule\Service\EventPersistence;
use StudioSchedule\Service\EventService;

final readonly class EventRoutes
{
    public function __construct(private EventService $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public static function register(App $app, EventPersistence $store, ClockInterface $clock): void
    {
        $service = new EventService($store, $clock);
        $routes = new self($service);
        $app->get('/api/v1/events', static fn(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
            => Responses::json($response, ['items' => $service->upcoming()]));
        $app->get('/api/v1/events/{slug}', $routes->detail(...));
        $app->get('/api/v1/events/{slug}/image', $routes->image(...));
    }

    /** @param array{slug:string} $args */
    private function detail(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $slug = $args['slug'];
        if (($problem = self::slugProblem($request, $slug)) !== null) {
            return $problem;
        }
        $event = $this->service->bySlug($slug);

        return $event === null
            ? Responses::problem($request, 404, 'resource not found')
            : Responses::json($response, $event, $request->getMethod() === 'HEAD');
    }

    /** @param array{slug:string} $args */
    private function image(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $slug = $args['slug'];
        if (($problem = self::slugProblem($request, $slug)) !== null) {
            return $problem;
        }
        $asset = $this->service->image($slug);
        if ($asset === null) {
            return Responses::problem($request, 404, 'resource not found');
        }
        if (!isset($asset['data'])) {
            return Responses::problem($request, 404, 'no image');
        }
        if (Responses::etagMatches($request->getHeaderLine('If-None-Match'), '"' . $asset['digest'] . '"')) {
            $asset = ['digest' => $asset['digest']];
        }

        return Responses::image($response, $asset, $request->getMethod() === 'HEAD');
    }

    private static function slugProblem(ServerRequestInterface $request, string $slug): ?ResponseInterface
    {
        if (mb_strlen($slug, 'UTF-8') > 80) {
            return Responses::problem($request, 422, 'validation failed', errors: [
                ['location' => 'path.slug', 'message' => 'expected length <= 80'],
            ]);
        }
        // Slim decodes path separators before dispatching, but a slash cannot be part of an event slug.
        if (stripos($request->getUri()->getPath(), '%2f') !== false
            || str_contains($slug, "\0") || !mb_check_encoding($slug, 'UTF-8')) {
            return Responses::problem($request, 404, 'resource not found');
        }

        return null;
    }
}
