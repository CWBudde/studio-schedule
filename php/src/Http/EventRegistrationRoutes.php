<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\EventRegistrationReads;
use StudioSchedule\Service\SessionPrincipal;

final readonly class EventRegistrationRoutes
{
    public function __construct(private EventRegistrationReads $registrations) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->get('/api/v1/admin/events/{eventId}/registrations', $this->office(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->get('/api/v1/my/events', $this->mine(...));
    }

    /** @param array{eventId:string} $args */
    private function office(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = $args['eventId'];
        if (!Id::valid($id) || stripos($request->getUri()->getPath(), '%2f') !== false) {
            return Responses::problem($request, 422, 'validation failed', errors: [
                ['location' => 'path.eventId', 'message' => 'invalid ID'],
            ]);
        }
        $items = $this->registrations->office(strtolower($id));

        return $items === null ? Responses::problem($request, 404, 'resource not found')
            : Responses::json($response, ['items' => $items])->withHeader('Cache-Control', 'no-store');
    }

    private function mine(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        if ($request->getMethod() === 'HEAD') {
            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', 'GET');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }

        return Responses::json($response, ['items' => $this->registrations->mine($principal->personId)])
            ->withHeader('Cache-Control', 'no-store');
    }
}
