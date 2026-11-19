<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Http\Server\RequestHandlerInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\Attendance;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\SessionPrincipal;

final readonly class AttendanceRoutes
{
    public function __construct(private Attendance $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->post('/api/v1/bookings/{bookingId}/attendance', $this->set(...))->add(self::requireStaff(...));
        $app->post('/api/v1/lessons/{lessonId}/attendance/all', $this->markAll(...))->add(self::requireStaff(...));
    }

    /** @param array{bookingId:string} $args */
    private function set(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $body = CatalogAdminRoutes::fields($request, ['status']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $id = self::pathId($request, $args['bookingId'], 'bookingId');
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);
        $result = $this->service->set($principal, $id, CatalogAdminRoutes::text($body, 'status', 8, true));

        return Responses::json($response, $result)->withHeader('Cache-Control', 'no-store');
    }

    /** @param array{lessonId:string} $args */
    private function markAll(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = self::pathId($request, $args['lessonId'], 'lessonId');
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);

        return Responses::json($response, ['items' => $this->service->markAll($principal, $id)])->withHeader('Cache-Control', 'no-store');
    }

    private static function pathId(ServerRequestInterface $request, string $id, string $field): string
    {
        if (!Id::valid($id) || stripos($request->getUri()->getPath(), '%2f') !== false) {
            throw new CatalogFailure(422, 'invalid ID', "path.$field");
        }

        return strtolower($id);
    }

    private static function requireStaff(#[SensitiveParameter] ServerRequestInterface $request, RequestHandlerInterface $next): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        if (!in_array($principal->role, ['instructor', 'office', 'admin'], true)) {
            return Responses::problem($request, 403, 'forbidden');
        }
        try {
            return $next->handle($request);
        } catch (CatalogFailure $failure) {
            return CatalogAdminRoutes::failure($request, $failure);
        }
    }
}
