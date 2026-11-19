<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Http\Server\RequestHandlerInterface;
use SensitiveParameter;
use Slim\App;
use Slim\Routing\RouteCollectorProxy;
use stdClass;
use StudioSchedule\Service\AdminSettings;
use StudioSchedule\Service\AdminSettingsFieldError;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;

final readonly class AdminSettingsRoutes
{
    public function __construct(private AdminSettings $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->group('/api/v1/admin/settings', function (RouteCollectorProxy $routes): void {
            $routes->map(['GET', 'PUT'], '/school', $this->school(...));
            $routes->map(['GET', 'PUT'], '/branding', $this->branding(...));
            $routes->map(['PUT', 'DELETE'], '/branding/background', $this->background(...));
        })->add(self::authorize(...));
    }

    private static function authorize(#[SensitiveParameter] ServerRequestInterface $request, RequestHandlerInterface $next): ResponseInterface
    {
        $method = $request->getMethod();
        // Slim dispatches HEAD to GET. The v1 settings cards expose GET and PUT.
        if ($method === 'HEAD') {
            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', 'GET, PUT');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        if ($principal->role !== 'admin' && ($principal->role !== 'office' || $method !== 'GET')) {
            return Responses::problem($request, 403, 'forbidden');
        }

        try {
            return $next->handle($request);
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        } catch (AdminSettingsFieldError $error) {
            return Responses::problem($request, 422, 'validation failed', errors: [
                ['location' => $error->field === '' ? 'body' : 'body.' . $error->field, 'message' => $error->getMessage()],
            ]);
        }
    }

    private function school(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        if ($request->getMethod() !== 'PUT') {
            return Responses::json($response, $this->service->school());
        }
        $body = JsonObject::read($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $fields = self::fields($body, ['schoolname', 'legalnotice']);
        $name = self::text($fields, 'schoolName', required: true);
        $notice = self::text($fields, 'legalNotice');

        return Responses::json($response, $this->service->updateSchool($name, $notice ?? ''));
    }

    private function branding(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        if ($request->getMethod() !== 'PUT') {
            return Responses::json($response, $this->service->branding());
        }
        $body = JsonObject::read($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $fields = self::fields($body, ['accentcolor', 'theme']);
        $accent = self::text($fields, 'accentColor');
        $theme = self::text($fields, 'theme');

        return Responses::json($response, $this->service->updateBranding($accent ?? '', $theme));
    }

    private function background(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        if ($request->getMethod() === 'DELETE') {
            $this->service->deleteBackground();

            return $response->withStatus(204);
        }
        $body = JsonObject::read($request, AdminSettings::MAX_BACKGROUND_BODY);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $fields = self::fields($body, ['contenttype', 'data']);
        $this->service->updateBackground(self::text($fields, 'contentType', required: true), self::text($fields, 'data', required: true));

        return $response->withStatus(204);
    }

    /**
     * @param list<string> $allowed
     * @return array<array-key, mixed>
     */
    private static function fields(stdClass $body, array $allowed): array
    {
        $fields = [];
        foreach (get_object_vars($body) as $key => $value) {
            $normalized = strtolower((string) $key);
            if (!in_array($normalized, $allowed, true)) {
                throw new AdminSettingsFieldError((string) $key, 'unexpected property');
            }
            $fields[$normalized] = $value;
        }

        return $fields;
    }

    /**
     * @param array<array-key, mixed> $fields
     * @return ($required is true ? string : ?string)
     */
    private static function text(array $fields, string $field, bool $required = false): ?string
    {
        $key = strtolower($field);
        if ($required && !array_key_exists($key, $fields)) {
            throw new AdminSettingsFieldError('', "expected required property $field to be present");
        }
        $value = $fields[$key] ?? null;
        if ($value === null && !$required) {
            return null;
        }
        if (!is_string($value)) {
            throw new AdminSettingsFieldError($field, 'expected string');
        }

        return $value;
    }
}
