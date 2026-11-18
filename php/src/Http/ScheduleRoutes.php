<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use InvalidArgumentException;
use Psr\Clock\ClockInterface;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Slim\App;
use StudioSchedule\Domain\Date;
use StudioSchedule\Service\SchedulePersistence;
use StudioSchedule\Service\ScheduleService;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;

final readonly class ScheduleRoutes
{
    public function __construct(private ScheduleService $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public static function register(App $app, SchedulePersistence $store, ClockInterface $clock): void
    {
        $routes = new self(new ScheduleService($store, $clock));
        $app->get('/api/v1/schedule', $routes->schedule(...));
        $app->get('/api/v1/lessons/{lessonId}', $routes->lesson(...));
    }

    public function schedule(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $params = self::queryParams($request);
        $errors = [];
        $dates = [];
        foreach (['from', 'to'] as $name) {
            $value = $params[$name] ?? '';
            if ($value === '') {
                $errors[] = ['location' => "query.$name", 'message' => 'required query parameter is missing'];
                continue;
            }
            try {
                $dates[$name] = Date::parse($value);
            } catch (InvalidArgumentException) {
                $errors[] = self::invalidParam("query.$name", 'date', $value);
            }
        }

        $filters = [];
        foreach (['location', 'instructor', 'courseType', 'level'] as $name) {
            $value = $params[$name] ?? '';
            if ($value === '') {
                continue;
            }
            if (!self::validId($value)) {
                $errors[] = self::invalidParam("query.$name", 'ID', $value);
                continue;
            }
            $filters[$name] = strtolower($value);
        }

        if ($errors !== []) {
            return self::invalid($request, $response, $errors);
        }

        assert(isset($dates['from'], $dates['to']));

        $error = ScheduleService::dateRangeError($dates['from'], $dates['to']);
        if ($error !== null) {
            return Responses::problem($request, 422, 'validation failed: ' . $error);
        }

        try {
            $schedule = $this->service->schedule($dates['from'], $dates['to'], $filters, self::principal($request));
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        }

        return Responses::json($response, $schedule);
    }

    /** @param array{lessonId: string} $args */
    public function lesson(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = $args['lessonId'];
        if (!self::validId($id)) {
            return self::invalid($request, $response, [self::invalidParam('path.lessonId', 'ID', $id)]);
        }

        try {
            $lesson = $this->service->lesson(strtolower($id), self::principal($request));
        } catch (SettingsNotFound) {
            $lesson = null;
        }

        return $lesson === null
            ? Responses::problem($request, 404, 'resource not found')
            : Responses::json($response, $lesson);
    }

    /** @return array<string, string> */
    private static function queryParams(ServerRequestInterface $request): array
    {
        $params = [];
        // The Go router reads the first value of each name. parse_str() also
        // interprets bracketed names as arrays and renames keys with spaces.
        foreach (explode('&', $request->getUri()->getQuery()) as $encoded) {
            $pair = explode('=', $encoded, 2);
            $name = urldecode($pair[0]);
            $params[$name] ??= isset($pair[1]) ? urldecode($pair[1]) : 'true';
        }

        return $params;
    }

    private static function principal(ServerRequestInterface $request): ?SessionPrincipal
    {
        $principal = $request->getAttribute('sessionPrincipal');

        return $principal instanceof SessionPrincipal ? $principal : null;
    }

    private static function validId(string $value): bool
    {
        return $value !== '00000000-0000-0000-0000-000000000000'
            && preg_match('/\A[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\z/i', $value) === 1;
    }

    /** @return array{location: string, message: string} */
    private static function invalidParam(string $location, string $type, string $value): array
    {
        $quoted = json_encode($value, JSON_THROW_ON_ERROR | JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE);

        return ['location' => $location, 'message' => "invalid value: validation failed: invalid $type $quoted"];
    }

    /** @param list<array{location: string, message: string}> $errors */
    private static function invalid(ServerRequestInterface $request, ResponseInterface $response, array $errors): ResponseInterface
    {
        $response = $response->withStatus(422);
        $body = [
            'title' => $response->getReasonPhrase(),
            'status' => 422,
            'detail' => 'validation failed',
            'errors' => $errors,
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
