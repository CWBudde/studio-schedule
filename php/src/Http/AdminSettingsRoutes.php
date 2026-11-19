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
use Slim\Routing\RouteContext;
use stdClass;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\TimeOfDay;
use StudioSchedule\Service\AdminSettings;
use StudioSchedule\Service\AdminSettingsConflict;
use StudioSchedule\Service\AdminSettingsFieldError;
use StudioSchedule\Service\BookingRuleSettings;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;

final readonly class AdminSettingsRoutes
{
    public function __construct(private AdminSettings $service, private BookingRuleSettings $bookingRules) {}

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
            $routes->map(['GET', 'PUT'], '/regional', $this->regional(...));
            $routes->map(['GET', 'PUT'], '/receipts', $this->receipts(...));
            $routes->map(['GET', 'PUT', 'DELETE'], '/receipts/logo', $this->receiptLogo(...));
            $routes->map(['GET', 'PUT'], '/booking-rules', $this->bookingRules(...));
            $routes->get('/booking-rules/preview', $this->preview(...));
        })->add(self::authorize(...));
    }

    private static function authorize(#[SensitiveParameter] ServerRequestInterface $request, RequestHandlerInterface $next): ResponseInterface
    {
        $method = $request->getMethod();
        // Slim dispatches HEAD to GET. Only the methods registered in v1 are allowed.
        if ($method === 'HEAD') {
            $allowed = RouteContext::fromRequest($request)->getRoute()?->getMethods() ?? ['GET'];

            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', implode(', ', $allowed));
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
        } catch (AdminSettingsConflict $error) {
            return Responses::problem($request, 409, $error->getMessage(), reason: $error->reason);
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

    private function receipts(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        if ($request->getMethod() === 'GET') {
            return Responses::json($response, $this->service->receiptSettings());
        }
        $body = JsonObject::read($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $names = array_keys(AdminSettings::RECEIPT_TEXT_LIMITS);
        $fields = self::fields($body, array_map(strtolower(...), $names));
        $input = [];
        foreach ($names as $field) {
            $value = self::text($fields, $field, required: $field === 'receiptPrefix');
            if ($value !== null) {
                $input[$field] = $value;
            }
        }

        return Responses::json($response, $this->service->updateReceiptSettings($input));
    }

    private function receiptLogo(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        if ($request->getMethod() === 'GET') {
            $image = $this->service->receiptLogo();
            if ($image === null) {
                return Responses::problem($request, 404, 'no logo');
            }
            $response->getBody()->write($image['data']);

            return $response->withHeader('Content-Type', $image['type'])
                ->withHeader('Content-Disposition', 'inline')->withHeader('Cache-Control', 'no-cache');
        }
        if ($request->getMethod() === 'DELETE') {
            $this->service->deleteReceiptLogo();

            return $response->withStatus(204);
        }
        $body = JsonObject::read($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $fields = self::fields($body, ['contenttype', 'data']);
        $this->service->updateReceiptLogo(self::text($fields, 'contentType', required: true), self::text($fields, 'data', required: true));

        return $response->withStatus(204);
    }

    private function regional(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        if ($request->getMethod() !== 'PUT') {
            return Responses::json($response, $this->service->regional());
        }
        $body = JsonObject::read($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $fields = self::fields($body, ['timezone', 'defaultlocale', 'currency']);

        return Responses::json($response, $this->service->updateRegional(
            self::text($fields, 'timezone', required: true),
            self::text($fields, 'defaultLocale', required: true),
            self::text($fields, 'currency', required: true),
        ));
    }

    private function bookingRules(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        if ($request->getMethod() !== 'PUT') {
            return Responses::json($response, $this->bookingRules->read());
        }
        $body = JsonObject::read($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $fields = self::fields($body, [
            'releaseweekday', 'releasetime', 'releaseweeksahead', 'releasedthrough', 'bookingcutoffminutes',
            'cancellationcutoffminutes', 'waitlistinviteexpiryminutes', 'defaultvisibledays',
        ]);
        $time = self::text($fields, 'releaseTime', required: true);
        $through = self::text($fields, 'releasedThrough');
        try {
            $at = TimeOfDay::parse($time);
        } catch (InvalidArgumentException) {
            throw new AdminSettingsFieldError('releaseTime', 'invalid time of day');
        }
        try {
            $date = $through === null ? null : Date::parse($through);
        } catch (InvalidArgumentException) {
            throw new AdminSettingsFieldError('releasedThrough', 'invalid released-through date');
        }

        return Responses::json($response, $this->bookingRules->update(
            self::bodyInteger($fields, 'releaseWeekday', 0, 6),
            $at,
            self::bodyInteger($fields, 'releaseWeeksAhead', 1, 52),
            $date,
            self::bodyInteger($fields, 'bookingCutoffMinutes', 0, 43200),
            self::bodyInteger($fields, 'cancellationCutoffMinutes', 0, 43200),
            self::bodyInteger($fields, 'waitlistInviteExpiryMinutes', 1, 43200),
            self::bodyInteger($fields, 'defaultVisibleDays', 1, 366),
        ));
    }

    private function preview(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $query = QueryParameters::read($request);
        $weekday = self::queryInteger($query['releaseWeekday'] ?? '', 0, 6);
        $time = $query['releaseTime'] ?? '';
        $validTime = preg_match('/\A[0-2][0-9]:[0-5][0-9]\z/', $time) === 1;
        $weeks = self::queryInteger($query['releaseWeeksAhead'] ?? '', 1, 52);
        try {
            $through = Date::fromText($query['releasedThrough'] ?? '');
        } catch (InvalidArgumentException) {
            $through = null;
        }
        if ($weekday === null || !$validTime || $weeks === null || $through === null) {
            $errors = [];
            foreach ([
                'releaseWeekday' => $weekday === null ? 'expected an integer between 0 and 6' : null,
                'releaseTime' => !$validTime ? 'expected a time in HH:MM form' : null,
                'releaseWeeksAhead' => $weeks === null ? 'expected an integer between 1 and 52' : null,
                'releasedThrough' => $through === null ? 'invalid released-through date' : null,
            ] as $field => $message) {
                if ($message !== null) {
                    $errors[] = ['location' => 'query.' . $field, 'message' => $message];
                }
            }

            return Responses::problem($request, 422, 'validation failed', errors: $errors);
        }

        try {
            $at = TimeOfDay::parse($time);
        } catch (InvalidArgumentException) {
            return Responses::problem($request, 422, 'validation failed', errors: [
                ['location' => 'query.releaseTime', 'message' => 'invalid time of day'],
            ]);
        }

        try {
            return Responses::json($response, $this->bookingRules->preview($weekday, $at, $weeks, $through->isZero() ? null : $through));
        } catch (AdminSettingsFieldError $error) {
            return Responses::problem($request, 422, 'validation failed', errors: [
                ['location' => 'query.' . $error->field, 'message' => $error->getMessage()],
            ]);
        }
    }

    private static function queryInteger(string $text, int $min, int $max): ?int
    {
        $value = (int) $text;

        return preg_match('/\A[+-]?[0-9]+\z/', $text) === 1 && $value >= $min && $value <= $max ? $value : null;
    }

    /** @param array<array-key, mixed> $fields */
    private static function bodyInteger(array $fields, string $field, int $min, int $max): int
    {
        $key = strtolower($field);
        if (!array_key_exists($key, $fields)) {
            throw new AdminSettingsFieldError('', "expected required property $field to be present");
        }
        $value = $fields[$key];
        if (!is_int($value) || $value < $min || $value > $max) {
            throw new AdminSettingsFieldError($field, "expected an integer between $min and $max");
        }

        return $value;
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
