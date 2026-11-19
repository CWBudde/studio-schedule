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
use StudioSchedule\Domain\Instant;
use StudioSchedule\Domain\TimeOfDay;
use StudioSchedule\Service\CatalogAdmin;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\ClassAdmin;
use StudioSchedule\Service\ClassData;
use StudioSchedule\Service\LessonRoomTaken;
use StudioSchedule\Service\OneOffLesson;
use StudioSchedule\Service\OneOffLessonData;
use StudioSchedule\Service\SetupInput;
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
                $idField = $resource->idField();
                $routes->map(['PUT', 'DELETE'], $path . '/{' . $idField . '}', fn(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
                    => $this->write($request, $response, $resource, self::id($args[$idField] ?? null, 'path.' . $idField)));
            }
            $routes->get('/admin/pass-types', fn(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
                => Responses::json($response, ['items' => $this->service->passTypes()]));
            $routes->post('/admin/pass-types', fn(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
                => $this->writePassType($request, $response, null));
            $routes->map(['PUT', 'DELETE'], '/admin/pass-types/{passTypeId}', fn(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
                => $this->writePassType($request, $response, self::id($args['passTypeId'] ?? null, 'path.passTypeId')));
            $routes->post('/admin/pass-types/{passTypeId}/retire', fn(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
                => Responses::json($response, $this->service->retirePassType(self::id($args['passTypeId'] ?? null, 'path.passTypeId'))));
            $routes->put('/locations/{locationId}/room-order', $this->roomOrder(...));
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
            $routes->map(['PUT', 'DELETE'], '/{termId}', static fn(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
                => self::writeTerm($request, $response, $service, self::id($args['termId'] ?? null, 'path.termId')));
        })->add(self::requireOffice(...));
    }

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public static function registerClasses(App $app, ClassAdmin $service): void
    {
        $app->group('/api/v1', static function (RouteCollectorProxy $routes) use ($service): void {
            $routes->post('/terms/{termId}/classes', static fn(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
                => self::createClass($request, $response, $service, self::id($args['termId'] ?? null, 'path.termId')));
            $routes->delete('/classes/{classId}', static function (ServerRequestInterface $request, ResponseInterface $response, array $args) use ($service): ResponseInterface {
                $service->delete(self::id($args['classId'] ?? null, 'path.classId'));

                return $response->withStatus(204);
            });
        })->add(self::requireOffice(...));
    }

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public static function registerLessonCreation(App $app, OneOffLesson $service): void
    {
        $app->post('/api/v1/lessons', static fn(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
            => self::createLesson($request, $response, $service))->add(self::requireOffice(...));
    }

    private static function createLesson(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, OneOffLesson $service): ResponseInterface
    {
        $body = self::fields($request, [
            'date', 'starttime', 'durationminutes', 'roomid', 'coursetypeid', 'instructorid', 'accompanistid',
            'capacity', 'visibility', 'kind', 'renter', 'confirmroomoverlap', 'confirmpast',
        ]);
        if ($body instanceof ResponseInterface) {
            return $body;
        }

        $date = self::date($body, 'date');
        try {
            $time = TimeOfDay::parse(self::text($body, 'startTime', 5, true));
        } catch (InvalidArgumentException) {
            throw CatalogFailure::field('startTime', 'expected a wall-clock time in HH:MM form');
        }
        $visibility = self::text($body, 'visibility', 7, true);
        if (!in_array($visibility, ['public', 'members', 'hidden'], true)) {
            throw CatalogFailure::field('visibility', 'invalid lesson visibility');
        }
        $kind = self::text($body, 'kind', 8, true);
        if (!in_array($kind, ['regular', 'workshop', 'private', 'rental'], true)) {
            throw CatalogFailure::field('kind', 'invalid lesson kind');
        }
        $data = new OneOffLessonData(
            date: $date,
            startTime: $time,
            durationMinutes: self::number($body, 'durationMinutes', 1, 1440, true),
            roomId: self::id(self::value($body, 'roomId', true), 'body.roomId'),
            courseTypeId: self::optionalId($body, 'courseTypeId'),
            instructorId: self::optionalId($body, 'instructorId'),
            accompanistId: self::optionalId($body, 'accompanistId'),
            capacity: self::number($body, 'capacity', 0, 10000, true),
            visibility: $visibility,
            kind: $kind,
            renter: SetupInput::trim(self::text($body, 'renter', 100) ?? ''),
            confirmRoomOverlap: self::flag($body, 'confirmRoomOverlap'),
            confirmPast: self::flag($body, 'confirmPast'),
        );
        try {
            return Responses::json($response->withStatus(201), $service->create($data))->withHeader('Cache-Control', 'no-store');
        } catch (LessonRoomTaken $error) {
            return self::roomTaken($request, $error);
        }
    }

    public static function roomTaken(#[SensitiveParameter] ServerRequestInterface $request, LessonRoomTaken $error): ResponseInterface
    {
        $overlap = $error->lesson;
        $lesson = ['id' => $overlap->id, 'startsAt' => Instant::format($overlap->startsAt), 'endsAt' => Instant::format($overlap->endsAt)];
        if ($overlap->courseType !== null) {
            $lesson['courseType'] = $overlap->courseType;
        }

        return Responses::problem($request, 409, $error->getMessage(), reason: 'room-taken', extensions: ['lesson' => $lesson]);
    }

    private static function createClass(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, ClassAdmin $service, string $term): ResponseInterface
    {
        $body = self::fields($request, [
            'coursetypeid', 'roomid', 'instructorid', 'accompanistid', 'levelfromid', 'leveltoid',
            'alllevels', 'stage', 'agefrommonths', 'agetomonths', 'weekday', 'starttime', 'durationminutes', 'capacity', 'retired',
        ]);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        try {
            $time = TimeOfDay::parse(self::text($body, 'startTime', 5, true));
        } catch (InvalidArgumentException) {
            throw CatalogFailure::field('startTime', 'expected a wall-clock time in HH:MM form');
        }
        $data = new ClassData(
            courseTypeId: self::id(self::value($body, 'courseTypeId', true), 'body.courseTypeId'),
            roomId: self::id(self::value($body, 'roomId', true), 'body.roomId'),
            instructorId: self::id(self::value($body, 'instructorId', true), 'body.instructorId'),
            weekday: self::number($body, 'weekday', 0, 6, true),
            startTime: $time,
            durationMinutes: self::number($body, 'durationMinutes', 1, 1440, true),
            capacity: self::number($body, 'capacity', 1, 1000, true),
            accompanistId: self::optionalId($body, 'accompanistId'),
            levelFromId: self::optionalId($body, 'levelFromId'),
            levelToId: self::optionalId($body, 'levelToId'),
            allLevels: self::flag($body, 'allLevels'),
            stage: self::number($body, 'stage', 1, 9),
            ageFromMonths: self::number($body, 'ageFromMonths', 1, 1200),
            ageToMonths: self::number($body, 'ageToMonths', 1, 1200),
            retired: self::flag($body, 'retired'),
        );

        return Responses::json($response->withStatus(201), $service->create($term, $data));
    }

    /** @param array<array-key,mixed> $body */
    public static function optionalId(array $body, string $field): ?string
    {
        $value = self::value($body, $field, false);

        return $value === null ? null : self::id($value, "body.$field");
    }

    /** @param array<array-key,mixed> $body */
    public static function flag(array $body, string $field): bool
    {
        $value = self::value($body, $field, false) ?? false;
        if (!is_bool($value)) {
            throw CatalogFailure::field($field, 'expected boolean');
        }

        return $value;
    }

    /**
     * @param array<array-key,mixed> $body
     * @return ($required is true ? int : ?int)
     */
    public static function number(array $body, string $field, int $min, int $max, bool $required = false): ?int
    {
        $value = self::value($body, $field, $required);
        if ($value === null && !$required) {
            return null;
        }
        if (!is_int($value) || $value < $min || $value > $max) {
            throw CatalogFailure::field($field, "expected an integer between $min and $max");
        }

        return $value;
    }

    private function writePassType(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, ?string $id): ResponseInterface
    {
        if ($request->getMethod() === 'DELETE' && $id !== null) {
            $this->service->deletePassType($id);

            return $response->withStatus(204);
        }
        $body = self::fields($request, ['name', 'kind', 'visits', 'validitydays', 'price', 'vatrate', 'active']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $kind = self::value($body, 'kind', true);
        if ($kind !== 'visits' && $kind !== 'time') {
            throw CatalogFailure::field('kind', 'invalid pass kind');
        }
        $active = self::value($body, 'active', true);
        if (!is_bool($active)) {
            throw CatalogFailure::field('active', 'expected boolean');
        }
        $pass = $this->service->savePassType($id, [
            'name' => self::text($body, 'name', 100, true),
            'kind' => $kind,
            'visits' => self::number($body, 'visits', 0, 1000) ?? 0,
            'validityDays' => self::number($body, 'validityDays', 0, 3660, true),
            'price' => self::number($body, 'price', 0, 100000000, true),
            'vatRate' => self::number($body, 'vatRate', 0, 10000, true),
            'active' => $active,
        ]);

        return Responses::json($response->withStatus($id === null ? 201 : 200), $pass);
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
    public static function date(array $body, string $field): Date
    {
        try {
            return Date::parse(self::text($body, $field, 10, true));
        } catch (InvalidArgumentException) {
            throw CatalogFailure::field($field, 'expected a calendar date in YYYY-MM-DD form');
        }
    }

    public static function requireOffice(#[SensitiveParameter] ServerRequestInterface $request, RequestHandlerInterface $next): ResponseInterface
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
            return self::failure($request, $failure);
        }
    }

    public static function failure(ServerRequestInterface $request, CatalogFailure $failure): ResponseInterface
    {
        if ($failure->status === 422 && $failure->location !== '') {
            return Responses::problem($request, 422, 'validation failed', errors: [
                ['location' => $failure->location, 'message' => $failure->getMessage()],
            ]);
        }

        return Responses::problem($request, $failure->status, $failure->getMessage(), reason: $failure->reason);
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

    /** @param array{locationId?: string} $args */
    private function roomOrder(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $location = self::id($args['locationId'] ?? null, 'path.locationId');
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
    public static function fields(#[SensitiveParameter] ServerRequestInterface $request, array $allowed, ?int $maxBytes = null): array|ResponseInterface
    {
        $body = $maxBytes === null ? JsonObject::read($request) : JsonObject::read($request, $maxBytes);
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
    public static function text(array $body, string $field, int $max, bool $required = false, bool $allowNul = false): ?string
    {
        $value = self::value($body, $field, $required);
        if ($value === null && !$required) {
            return null;
        }
        if (!is_string($value) || (!$allowNul && str_contains($value, "\0")) || ($required && $value === '') || mb_strlen($value, 'UTF-8') > $max) {
            throw CatalogFailure::field($field, "expected text of at most $max characters");
        }

        return $value;
    }

    /** @param array<array-key,mixed> $body */
    public static function requiredId(array $body, string $field): string
    {
        return self::id(self::value($body, $field, true), 'body.' . $field);
    }

    private static function id(mixed $value, string $location): string
    {
        if (!is_string($value) || !Id::valid($value)) {
            throw new CatalogFailure(422, 'invalid ID', $location);
        }

        return strtolower($value);
    }
}
