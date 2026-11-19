<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Closure;
use InvalidArgumentException;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Http\Server\RequestHandlerInterface;
use SensitiveParameter;
use Slim\App;
use Slim\Routing\RouteCollectorProxy;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Id;
use StudioSchedule\Domain\PersonData;
use StudioSchedule\Service\Family;
use StudioSchedule\Service\FamilyFieldError;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;

final readonly class FamilyRoutes
{
    public function __construct(private Family $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->group('/api/v1/family', function (RouteCollectorProxy $routes): void {
            $routes->get('', $this->get(...));
            $routes->put('/me', $this->update(...));
            $routes->post('/dependents', $this->add(...));
            $routes->put('/dependents/{personId}', $this->update(...));
        })->add(static function (#[SensitiveParameter] ServerRequestInterface $request, RequestHandlerInterface $next): ResponseInterface {
            if (!$request->getAttribute('sessionPrincipal') instanceof SessionPrincipal) {
                return Responses::problem($request, 401, 'not signed in');
            }

            return $next->handle($request);
        });
    }

    private function get(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $family = $this->service->get(self::personId($request));

        return $family === null ? Responses::problem($request, 404, 'resource not found')
            : Responses::json($response, $family, $request->getMethod() === 'HEAD');
    }

    private function add(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $guardianId = self::personId($request);

        return $this->change($request, $response->withStatus(201), fn(PersonData $data): ?array => $this->service->addDependent($guardianId, $data));
    }

    /** @param array{personId?:string} $args */
    private function update(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $caller = self::personId($request);
        $dependent = $args['personId'] ?? null;
        if ($dependent !== null && !Id::valid($dependent)) {
            return Responses::problem($request, 422, 'validation failed', errors: [['location' => 'path.personId', 'message' => 'invalid ID']]);
        }
        $id = $dependent === null ? $caller : strtolower($dependent);
        $guardian = $dependent === null ? null : $caller;

        return $this->change($request, $response, fn(PersonData $data): array|string|null => $this->service->update($id, $data, $guardian));
    }

    /** @param Closure(PersonData):(array<string,mixed>|'managed'|null) $write */
    private function change(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, Closure $write): ResponseInterface
    {
        $data = self::input($request);
        if ($data instanceof ResponseInterface) {
            return $data;
        }
        try {
            $result = $write($data);
        } catch (FamilyFieldError $error) {
            return Responses::problem($request, 422, 'validation failed', errors: [
                ['location' => 'body.' . $error->field, 'message' => $error->getMessage()],
            ]);
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        }

        return match ($result) {
            null => Responses::problem($request, 404, 'resource not found'),
            'managed' => Responses::problem($request, 409, "the dependent's data is managed by its own account or the office", reason: 'managed'),
            default => Responses::json($response, $result),
        };
    }

    private static function input(#[SensitiveParameter] ServerRequestInterface $request): PersonData|ResponseInterface
    {
        $body = JsonObject::read($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $raw = get_object_vars($body);
        $fields = array_change_key_case($raw);
        $errors = [];
        $limits = ['givenName' => [1, 100, true], 'familyName' => [1, 100, true], 'phone' => [0, 50, false], 'birthDate' => [10, 10, false]];
        $values = [];
        foreach ($limits as $field => [$min, $max, $required]) {
            $key = strtolower($field);
            $value = $fields[$key] ?? null;
            if ($value === null && !$required) {
                $values[$field] = '';
                continue;
            }
            if (!array_key_exists($key, $fields)) {
                $errors[] = ['location' => 'body', 'message' => "expected required property $field to be present"];
                continue;
            }
            if (!is_string($value) || str_contains($value, "\0") || mb_strlen($value, 'UTF-8') < $min || mb_strlen($value, 'UTF-8') > $max) {
                $errors[] = ['location' => "body.$field", 'message' => "expected text of $min to $max characters"];
            } else {
                $values[$field] = $value;
            }
        }
        $keys = array_change_key_case($limits);
        foreach (array_keys($raw) as $field) {
            if (!isset($keys[strtolower((string) $field)])) {
                $errors[] = ['location' => "body.$field", 'message' => 'unexpected property'];
            }
        }
        if ($errors !== []) {
            return Responses::problem($request, 422, 'validation failed', errors: $errors);
        }
        assert(isset($values['givenName'], $values['familyName'], $values['phone'], $values['birthDate']));
        try {
            $date = Date::fromText($values['birthDate']);
        } catch (InvalidArgumentException) {
            return Responses::problem($request, 422, 'validation failed', errors: [
                ['location' => 'body.birthDate', 'message' => 'expected a calendar date in YYYY-MM-DD form'],
            ]);
        }

        return new PersonData($values['givenName'], $values['familyName'], $values['phone'], $date);
    }

    private static function personId(#[SensitiveParameter] ServerRequestInterface $request): string
    {
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);

        return $principal->personId;
    }
}
