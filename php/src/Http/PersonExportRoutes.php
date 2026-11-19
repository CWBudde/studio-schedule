<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\PersonExport;
use StudioSchedule\Service\SessionPrincipal;

final readonly class PersonExportRoutes
{
    public function __construct(private PersonExport $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->get('/api/v1/my/export', $this->family(...));
        $app->get('/api/v1/admin/persons/{personId}/export', $this->office(...));
    }

    private function family(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        $id = QueryParameters::read($request)['person'] ?? '';
        if (!Id::valid($id)) {
            return self::invalid($request, 'query.person', $id === '' ? 'required query parameter is missing' : 'invalid ID');
        }

        return $this->export($request, $response, strtolower($id), $principal->personId);
    }

    /** @param array{personId:string} $args */
    private function office(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        if (!in_array($principal->role, ['office', 'admin'], true)) {
            return Responses::problem($request, 403, 'forbidden');
        }
        if (!Id::valid($args['personId'])) {
            return self::invalid($request, 'path.personId', 'invalid ID');
        }

        return $this->export($request, $response, strtolower($args['personId']), null);
    }

    private function export(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, string $id, ?string $familyPerson): ResponseInterface
    {
        $document = $this->service->document($id, $familyPerson);

        return $document === null ? Responses::problem($request, 404, 'resource not found')
            : Responses::json($response, $document, $request->getMethod() === 'HEAD')->withHeader('Cache-Control', 'no-store');
    }

    private static function invalid(ServerRequestInterface $request, string $location, string $message): ResponseInterface
    {
        return Responses::problem($request, 422, 'validation failed', errors: [['location' => $location, 'message' => $message]]);
    }
}
