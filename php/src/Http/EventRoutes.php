<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use DateTimeImmutable;
use DateTimeZone;
use InvalidArgumentException;
use Psr\Clock\ClockInterface;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Slim\App;
use StudioSchedule\Domain\Id;
use StudioSchedule\Domain\TimeOfDay;
use StudioSchedule\Service\AdminSettings;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\EventData;
use StudioSchedule\Service\EventPersistence;
use StudioSchedule\Service\EventService;
use StudioSchedule\Service\EventSessionData;
use StudioSchedule\Service\LessonRoomTaken;
use StudioSchedule\Service\OneOffLesson;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SetupInput;

final readonly class EventRoutes
{
    public function __construct(private EventService $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public static function register(App $app, EventPersistence $store, ClockInterface $clock, OneOffLesson $lessons): void
    {
        $service = new EventService($store, $clock, $lessons);
        $routes = new self($service);
        $app->get('/api/v1/events', static fn(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
            => Responses::json($response, ['items' => $service->upcoming()]));
        $app->get('/api/v1/events/{slug}', $routes->detail(...));
        $app->get('/api/v1/events/{slug}/image', $routes->image(...));
        $app->get('/api/v1/admin/events', static fn(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
            => Responses::json($response, ['items' => $service->officeList()])->withHeader('Cache-Control', 'no-store'))
            ->add(CatalogAdminRoutes::requireOffice(...));
        $app->get('/api/v1/admin/events/{eventId}', $routes->officeDetail(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->get('/api/v1/admin/events/{eventId}/image', $routes->officeImage(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->post('/api/v1/admin/events', $routes->createDraft(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->delete('/api/v1/admin/events/{eventId}', $routes->delete(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->post('/api/v1/admin/events/{eventId}/sessions', $routes->addSession(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->post('/api/v1/admin/events/{eventId}/publish', $routes->publish(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->post('/api/v1/admin/events/{eventId}/unpublish', $routes->unpublish(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->put('/api/v1/admin/events/{eventId}/image', $routes->uploadImage(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->delete('/api/v1/admin/events/{eventId}/image', $routes->deleteImage(...))->add(CatalogAdminRoutes::requireOffice(...));
    }

    private function createDraft(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $body = CatalogAdminRoutes::fields($request, [
            'slug', 'title', 'description', 'coursetypeid', 'capacity', 'price', 'vatrate', 'signupopensat', 'signupclosesat', 'canceluntil',
        ]);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);
        $description = SetupInput::trim(CatalogAdminRoutes::text($body, 'description', 10000) ?? '');
        $data = new EventData(
            slug: SetupInput::trim(CatalogAdminRoutes::text($body, 'slug', 80) ?? ''),
            title: SetupInput::trim(CatalogAdminRoutes::text($body, 'title', 200, true)),
            description: $description === '' ? null : $description,
            courseTypeId: CatalogAdminRoutes::requiredId($body, 'courseTypeId'),
            capacity: CatalogAdminRoutes::number($body, 'capacity', 1, 10000, true),
            price: CatalogAdminRoutes::number($body, 'price', 0, 100000000, true),
            vatRate: CatalogAdminRoutes::number($body, 'vatRate', 0, 10000, true),
            signupOpensAt: self::instant($body['signupopensat'] ?? null, 'signupOpensAt'),
            signupClosesAt: self::instant($body['signupclosesat'] ?? null, 'signupClosesAt'),
            cancelUntil: self::instant($body['canceluntil'] ?? null, 'cancelUntil'),
        );

        return Responses::json($response->withStatus(201), $this->service->createDraft($data, $principal))
            ->withHeader('Cache-Control', 'no-store');
    }

    /** @param array{eventId:string} $args */
    private function addSession(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = self::officeId($request, $args['eventId']);
        $body = CatalogAdminRoutes::fields($request, [
            'date', 'starttime', 'durationminutes', 'roomid', 'instructorid', 'accompanistid', 'confirmroomoverlap', 'confirmpast',
        ]);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        try {
            $time = TimeOfDay::parse(CatalogAdminRoutes::text($body, 'startTime', 5, true));
        } catch (InvalidArgumentException) {
            throw CatalogFailure::field('startTime', 'expected a wall-clock time in HH:MM form');
        }
        $data = new EventSessionData(
            date: CatalogAdminRoutes::date($body, 'date'),
            startTime: $time,
            durationMinutes: CatalogAdminRoutes::number($body, 'durationMinutes', 1, 1440, true),
            roomId: CatalogAdminRoutes::requiredId($body, 'roomId'),
            instructorId: CatalogAdminRoutes::requiredId($body, 'instructorId'),
            accompanistId: CatalogAdminRoutes::optionalId($body, 'accompanistId'),
            confirmRoomOverlap: CatalogAdminRoutes::flag($body, 'confirmRoomOverlap'),
            confirmPast: CatalogAdminRoutes::flag($body, 'confirmPast'),
        );
        try {
            return Responses::json($response->withStatus(201), $this->service->addSession($id, $data))->withHeader('Cache-Control', 'no-store');
        } catch (LessonRoomTaken $error) {
            return CatalogAdminRoutes::roomTaken($request, $error);
        }
    }

    /** @param array{eventId:string} $args */
    private function delete(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $this->service->delete(self::officeId($request, $args['eventId']));

        return $response->withStatus(204);
    }

    /** @param array{eventId:string} $args */
    private function publish(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        return Responses::json($response, $this->service->publish(self::officeId($request, $args['eventId']), true))
            ->withHeader('Cache-Control', 'no-store');
    }

    /** @param array{eventId:string} $args */
    private function unpublish(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        return Responses::json($response, $this->service->publish(self::officeId($request, $args['eventId']), false))
            ->withHeader('Cache-Control', 'no-store');
    }

    /** @param array{eventId:string} $args */
    private function uploadImage(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = self::officeId($request, $args['eventId']);
        $body = CatalogAdminRoutes::fields($request, ['contenttype', 'data'], AdminSettings::MAX_BACKGROUND_BODY);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $this->service->uploadImage(
            $id,
            CatalogAdminRoutes::text($body, 'contentType', 32, true),
            CatalogAdminRoutes::text($body, 'data', AdminSettings::MAX_BACKGROUND_BASE64, true),
        );

        return $response->withStatus(204);
    }

    /** @param array{eventId:string} $args */
    private function deleteImage(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $this->service->deleteImage(self::officeId($request, $args['eventId']));

        return $response->withStatus(204);
    }

    private static function instant(mixed $value, string $field): ?DateTimeImmutable
    {
        if ($value === null) {
            return null;
        }
        if (!is_string($value) || preg_match('/\A(\d{4})-(\d{2})-(\d{2})T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d+)?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)\z/', $value, $parts) !== 1
            || !checkdate((int) $parts[2], (int) $parts[3], (int) $parts[1])) {
            throw CatalogFailure::field($field, 'expected an RFC 3339 time');
        }

        $instant = (new DateTimeImmutable($value))->setTimezone(new DateTimeZone('UTC'));
        $year = (int) $instant->format('Y');
        if ($year < 1 || $year > 9999) {
            throw CatalogFailure::field($field, 'the time is outside the supported UTC date range');
        }

        return $instant == new DateTimeImmutable('0001-01-01T00:00:00Z') ? null : $instant;
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
        return self::imageResponse($request, $response, $this->service->image($slug));
    }

    /** @param array{eventId:string} $args */
    private function officeDetail(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $event = $this->service->officeDetail(self::officeId($request, $args['eventId']));

        return $event === null
            ? Responses::problem($request, 404, 'resource not found')
            : Responses::json($response, $event)->withHeader('Cache-Control', 'no-store');
    }

    /** @param array{eventId:string} $args */
    private function officeImage(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        return self::imageResponse($request, $response, $this->service->officeImage(self::officeId($request, $args['eventId'])));
    }

    private static function officeId(ServerRequestInterface $request, string $id): string
    {
        // Slim decodes separators before routing. An encoded slash belongs to the input ID.
        if (!Id::valid($id) || stripos($request->getUri()->getPath(), '%2f') !== false) {
            throw new CatalogFailure(422, 'invalid ID', 'path.eventId');
        }

        return strtolower($id);
    }

    /** @param array{digest:string,contentType:string,data:string}|array{data:null}|null $asset */
    private static function imageResponse(ServerRequestInterface $request, ResponseInterface $response, ?array $asset): ResponseInterface
    {
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

    public static function slugProblem(ServerRequestInterface $request, string $slug): ?ResponseInterface
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
