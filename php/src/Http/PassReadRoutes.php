<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\PassRead;
use StudioSchedule\Service\SessionPrincipal;

final readonly class PassReadRoutes
{
    public function __construct(private PassRead $passes) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->get('/api/v1/admin/persons/{personId}/passes', $this->person(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->get('/api/v1/my/passes', $this->mine(...));
    }

    /** @param array{personId:string} $args */
    private function person(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        if (!Id::valid($args['personId'])) {
            return Responses::problem($request, 422, 'validation failed', errors: [['location' => 'path.personId', 'message' => 'invalid ID']]);
        }
        $items = $this->passes->person($args['personId']);

        return $items === null ? Responses::problem($request, 404, 'resource not found') : Responses::json($response, ['items' => $items]);
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
        $result = $this->passes->mine($principal->personId);

        return $result === null ? Responses::problem($request, 404, 'resource not found') : Responses::json($response, $result);
    }
}
