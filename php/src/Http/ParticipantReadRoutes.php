<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\ParticipantRead;
use StudioSchedule\Service\SessionPrincipal;

final readonly class ParticipantReadRoutes
{
    public function __construct(private ParticipantRead $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->get('/api/v1/lessons/{lessonId}/participants', $this->roster(...));
        $app->get('/api/v1/admin/bookings/{bookingId}/events', $this->history(...))->add(CatalogAdminRoutes::requireOffice(...));
    }

    /** @param array<string, mixed> $args */
    private function roster(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        if ($request->getMethod() === 'HEAD') {
            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', 'GET');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        if (!in_array($principal->role, ['instructor', 'office', 'admin'], true)) {
            return Responses::problem($request, 403, 'forbidden');
        }
        $lesson = $args['lessonId'] ?? null;
        if (!is_string($lesson) || !Id::valid($lesson)) {
            return self::invalidId($request, 'path.lessonId');
        }

        return self::result($request, $response, $this->service->roster(strtolower($lesson), $principal));
    }

    /** @param array<string, mixed> $args */
    private function history(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $booking = $args['bookingId'] ?? null;
        if (!is_string($booking) || !Id::valid($booking)) {
            return self::invalidId($request, 'path.bookingId');
        }
        $events = $this->service->history(strtolower($booking));

        return self::result($request, $response, $events === null ? null : ['items' => $events]);
    }

    private static function invalidId(#[SensitiveParameter] ServerRequestInterface $request, string $field): ResponseInterface
    {
        return Responses::problem($request, 422, 'validation failed', errors: [['location' => $field, 'message' => 'invalid ID']]);
    }

    /** @param array<string, mixed>|null $body */
    private static function result(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, ?array $body): ResponseInterface
    {
        return $body === null ? Responses::problem($request, 404, 'resource not found')
            : Responses::json($response, $body)->withHeader('Cache-Control', 'no-store');
    }
}
