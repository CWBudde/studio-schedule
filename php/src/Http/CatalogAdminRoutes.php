<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use InvalidArgumentException;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Http\Server\RequestHandlerInterface;
use SensitiveParameter;
use Slim\App;
use Slim\Routing\RouteCollectorProxy;
use StudioSchedule\Domain\CatalogResource;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\CatalogAdmin;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\TermAdmin;

/** @phpstan-import-type CatalogData from \StudioSchedule\Service\CatalogAdminPersistence */
final readonly class CatalogAdminRoutes
{
    public function __construct(private CatalogAdmin $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->group('/api/v1', function (RouteCollectorProxy $routes): void {
            foreach (CatalogResource::cases() as $resource) {
                $path = '/' . $resource->value;
                $routes->post($path, fn(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
                    => $this->write($request, $response, $resource, null));
                $routes->map(['PUT', 'DELETE'], $path . '/{id}', fn(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
                    => $this->write($request, $response, $resource, self::id($args['id'] ?? null, 'path.' . $resource->idField())));
            }
            $routes->put('/locations/{id}/room-order', $this->roomOrder(...));
            $routes->put('/level-order', $this->levelOrder(...));
            $routes->get('/admin/staff', fn(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
                => Responses::json($response, ['items' => $this->service->allStaff()]));
        })->add(self::requireOffice(...));
    }

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public static function registerTerms(App $app, TermAdmin $service): void
    {
        $app->group('/api/v1/terms', static function (RouteCollectorProxy $routes) use ($service): void {
            $routes->post('', static fn(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
                => self::writeTerm($request, $response, $service, null));
            $routes->map(['PUT', 'DELETE'], '/{id}', static fn(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
                => self::writeTerm($request, $response, $service, self::id($args['id'] ?? null, 'path.termId')));
        })->add(self::requireOffice(...));
    }

    private static function writeTerm(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, TermAdmin $service, ?string $id): ResponseInterface
    {
        if ($request->getMethod() === 'DELETE' && $id !== null) {
            $service->delete($id);

            return $response->withStatus(204);
        }
        $body = self::fields($request, ['name', 'startson', 'endson']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $name = self::text($body, 'name', 100, true);
        $startsOn = self::date($body, 'startsOn');
        $endsOn = self::date($body, 'endsOn');

        return Responses::json($response->withStatus($id === null ? 201 : 200), $service->save($id, $name, $startsOn, $endsOn));
    }

    /** @param array<array-key,mixed> $body */
    private static function date(array $body, string $field): Date
    {
        try {
            return Date::parse(self::text($body, $field, 10, true));
        } catch (InvalidArgumentException) {
            throw CatalogFailure::field($field, 'expected a calendar date in YYYY-MM-DD form');
        }
    }

    private static function requireOffice(#[SensitiveParameter] ServerRequestInterface $request, RequestHandlerInterface $next): ResponseInterface
    {
        if ($request->getMethod() === 'HEAD') {
            // Slim falls back to GET, but the office list exposes GET only.
            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', 'GET');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        if (!in_array($principal->role, ['office', 'admin'], true)) {
            return Responses::problem($request, 403, 'forbidden');
        }

        try {
            return $next->handle($request);
        } catch (CatalogFailure $failure) {
            if ($failure->status === 422) {
                return Responses::problem($request, 422, 'validation failed', errors: [
                    ['location' => $failure->location, 'message' => $failure->getMessage()],
                ]);
            }

            return Responses::problem($request, $failure->status, $failure->getMessage(), reason: $failure->reason);
        }
    }

    private function write(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, CatalogResource $resource, ?string $id): ResponseInterface
    {
        if ($request->getMethod() === 'DELETE' && $id !== null) {
            $this->service->delete($resource, $id);

            return $response->withStatus(204);
        }
        $allowed = match ($resource) {
            CatalogResource::Location => ['name', 'address'],
            CatalogResource::Room => ['locationid', 'name', 'capacity'],
            CatalogResource::CourseType => ['name', 'description'],
            CatalogResource::Level => ['name', 'code'],
            CatalogResource::Staff => ['displayname', 'kind', 'personid', 'active'],
        };
        $body = self::fields($request, $allowed);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $record = self::record($resource, $body);

        return Responses::json($response->withStatus($id === null ? 201 : 200), $this->service->save($resource, $id, $record));
    }

    /**
     * @param array<array-key,mixed> $body
     * @return CatalogData
     */
    private static function record(CatalogResource $resource, array $body): array
    {
        if ($resource === CatalogResource::Staff) {
            $kind = self::text($body, 'kind', 20, true);
            if (!in_array($kind, ['instructor', 'accompanist'], true)) {
                throw CatalogFailure::field('kind', 'expected instructor or accompanist');
            }
            $active = self::value($body, 'active', true);
            if (!is_bool($active)) {
                throw CatalogFailure::field('active', 'expected boolean');
            }
            $person = $body['personid'] ?? null;

            return ['displayName' => self::text($body, 'displayName', 100, true), 'kind' => $kind, 'active' => $active,
                'personId' => $person === null ? null : self::id($person, 'body.personId')];
        }
        $name = self::text($body, 'name', $resource === CatalogResource::Level ? 60 : 100, true);
        if ($resource === CatalogResource::Room) {
            $capacity = self::value($body, 'capacity', true);
            if (!is_int($capacity) || $capacity < 1 || $capacity > 1000) {
                throw CatalogFailure::field('capacity', 'expected an integer between 1 and 1000');
            }

            return ['locationId' => self::id(self::value($body, 'locationId', true), 'body.locationId'), 'name' => $name, 'capacity' => $capacity];
        }

        return ['name' => $name] + match ($resource) {
            CatalogResource::Location => ['address' => self::text($body, 'address', 200)],
            CatalogResource::CourseType => ['description' => self::text($body, 'description', 2000)],
            CatalogResource::Level => ['code' => self::text($body, 'code', 8)],
        };
    }

    /** @param array{id?: string} $args */
    private function roomOrder(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $location = self::id($args['id'] ?? null, 'path.locationId');
        $body = self::fields($request, ['roomids']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $this->service->orderRooms($location, self::order($body, 'roomIds'));

        return $response->withStatus(204);
    }

    private function levelOrder(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $body = self::fields($request, ['levelids']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $this->service->orderLevels(self::order($body, 'levelIds'));

        return $response->withStatus(204);
    }

    /**
     * @param array<array-key,mixed> $body
     * @return list<string>
     */
    private static function order(array $body, string $field): array
    {
        $ids = self::value($body, $field, true);
        if (!is_array($ids) || !array_is_list($ids) || count($ids) > 1000) {
            throw CatalogFailure::field($field, 'expected a list of at most 1000 IDs');
        }
        $out = [];
        foreach ($ids as $index => $id) {
            $out[] = self::id($id, "body.$field" . "[$index]");
        }

        return $out;
    }

    /**
     * @param list<string> $allowed
     * @return array<array-key,mixed>|ResponseInterface
     */
    private static function fields(#[SensitiveParameter] ServerRequestInterface $request, array $allowed): array|ResponseInterface
    {
        $body = JsonObject::read($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $out = [];
        foreach (get_object_vars($body) as $key => $value) {
            $normalized = strtolower((string) $key);
            if (!in_array($normalized, $allowed, true)) {
                throw CatalogFailure::field((string) $key, 'unexpected property');
            }
            $out[$normalized] = $value;
        }

        return $out;
    }

    /** @param array<array-key,mixed> $body */
    private static function value(array $body, string $field, bool $required): mixed
    {
        $key = strtolower($field);
        if ($required && !array_key_exists($key, $body)) {
            throw new CatalogFailure(422, "expected required property $field to be present", 'body');
        }

        return $body[$key] ?? null;
    }

    /**
     * @param array<array-key,mixed> $body
     * @return ($required is true ? string : ?string)
     */
    private static function text(array $body, string $field, int $max, bool $required = false): ?string
    {
        $value = self::value($body, $field, $required);
        if ($value === null && !$required) {
            return null;
        }
        if (!is_string($value) || str_contains($value, "\0") || ($required && $value === '') || mb_strlen($value, 'UTF-8') > $max) {
            throw CatalogFailure::field($field, "expected text of at most $max characters");
        }

        return $value;
    }

    private static function id(mixed $value, string $location): string
    {
        if (!is_string($value) || !Id::valid($value)) {
            throw new CatalogFailure(422, 'invalid ID', $location);
        }

        return strtolower($value);
    }
}
