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

final class EventRoutes
{
    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public static function register(App $app, EventPersistence $store, ClockInterface $clock): void
    {
        $service = new EventService($store, $clock);
        $app->get('/api/v1/events', static fn(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
            => Responses::json($response, ['items' => $service->upcoming()]));
    }
}
