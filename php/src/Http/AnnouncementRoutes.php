<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Closure;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Http\Server\RequestHandlerInterface;
use SensitiveParameter;
use Slim\App;
use Slim\Routing\RouteCollectorProxy;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\AnnouncementInput;
use StudioSchedule\Service\AnnouncementProblem;
use StudioSchedule\Service\Announcements;
use StudioSchedule\Service\SessionPrincipal;

final readonly class AnnouncementRoutes
{
    public function __construct(private Announcements $announcements) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->group('/api/v1/announcements', function (RouteCollectorProxy $routes): void {
            $routes->get('', $this->mine(...));
            $routes->post('/{announcementId}/dismiss', $this->dismiss(...));
        })->add(self::requireSignIn(...));
        $app->group('/api/v1/admin/announcements', function (RouteCollectorProxy $routes): void {
            $routes->get('', $this->list(...));
            $routes->post('', $this->create(...));
            $routes->post('/recipients', $this->recipients(...));
            $routes->put('/{announcementId}', $this->update(...));
            $routes->delete('/{announcementId}', $this->delete(...));
        })->add(CatalogAdminRoutes::requireOffice(...));
    }

    private static function requireSignIn(ServerRequestInterface $request, RequestHandlerInterface $next): ResponseInterface
    {
        return $request->getAttribute('sessionPrincipal') instanceof SessionPrincipal
            ? $next->handle($request)
            : Responses::problem($request, 401, 'not signed in');
    }

    private function mine(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        return Responses::json($response, ['items' => $this->announcements->mine(self::principal($request))], $request->getMethod() === 'HEAD');
    }

    private function list(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        return Responses::json($response, ['items' => $this->announcements->listAll()], $request->getMethod() === 'HEAD');
    }

    /** @param array{announcementId:string} $args */
    private function dismiss(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        return self::respond($request, function () use ($request, $response, $args): ResponseInterface {
            $this->announcements->dismiss(self::principal($request), self::id($args['announcementId']));

            return $response->withStatus(204);
        });
    }

    private function create(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        return $this->save($request, $response->withStatus(201));
    }

    /** @param array{announcementId:string} $args */
    private function update(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        return $this->save($request, $response, $args['announcementId']);
    }

    /** @param array{announcementId:string} $args */
    private function delete(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        return self::respond($request, function () use ($response, $args): ResponseInterface {
            $this->announcements->delete(self::id($args['announcementId']));

            return $response->withStatus(204);
        });
    }

    private function save(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, ?string $id = null): ResponseInterface
    {
        return self::respond($request, function () use ($request, $response, $id): ResponseInterface {
            $id = $id === null ? null : self::id($id);
            $body = JsonObject::read($request);
            if ($body instanceof ResponseInterface) {
                return $body;
            }
            $input = AnnouncementInput::fromFields(get_object_vars($body));

            return Responses::json($response, $this->announcements->save(self::principal($request), $input, $id));
        });
    }

    private function recipients(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        return self::respond($request, function () use ($request, $response): ResponseInterface {
            $body = JsonObject::read($request);
            if ($body instanceof ResponseInterface) {
                return $body;
            }
            $audience = AnnouncementInput::audience(get_object_vars($body));

            return Responses::json($response, ['recipients' => $this->announcements->countRecipients($audience)]);
        });
    }

    private static function principal(ServerRequestInterface $request): SessionPrincipal
    {
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);

        return $principal;
    }

    private static function id(string $id): string
    {
        if (!Id::valid($id)) {
            throw AnnouncementProblem::field('path.announcementId', 'invalid ID');
        }

        return strtolower($id);
    }

    /** @param Closure():ResponseInterface $handle */
    private static function respond(ServerRequestInterface $request, Closure $handle): ResponseInterface
    {
        try {
            return $handle();
        } catch (AnnouncementProblem $problem) {
            if ($problem->fieldName !== '') {
                $location = str_starts_with($problem->fieldName, 'path.') ? $problem->fieldName : 'body.' . $problem->fieldName;
                $message = $problem->getMessage();

                return Responses::problem($request, 422, 'validation failed', errors: [['location' => $location, 'message' => $message]]);
            }

            return Responses::problem($request, $problem->status, $problem->getMessage(), reason: $problem->reason);
        }
    }
}
