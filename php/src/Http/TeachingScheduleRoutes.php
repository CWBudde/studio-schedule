<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use InvalidArgumentException;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;
use StudioSchedule\Service\TeachingSchedule;

final readonly class TeachingScheduleRoutes
{
    public function __construct(private TeachingSchedule $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->get('/api/v1/teach', $this->view(...));
    }

    private function view(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
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

        $query = QueryParameters::read($request);
        $dates = [];
        $errors = [];
        foreach (['from', 'to'] as $field) {
            $value = $query[$field] ?? '';
            if ($value === '') {
                continue;
            }
            try {
                $dates[$field] = Date::parse($value);
            } catch (InvalidArgumentException) {
                $errors[] = ['location' => "query.$field", 'message' => 'expected a school date in YYYY-MM-DD form'];
            }
        }
        $staff = $query['staffId'] ?? '';
        if ($staff !== '' && !Id::valid($staff)) {
            $errors[] = ['location' => 'query.staffId', 'message' => 'invalid ID'];
        }
        if ($errors !== []) {
            return Responses::validation($request, $response, $errors);
        }

        try {
            $view = $this->service->view($principal, $dates['from'] ?? null, $dates['to'] ?? null, $staff === '' ? null : strtolower($staff));

            return Responses::json($response, $view)->withHeader('Cache-Control', 'no-store');
        } catch (CatalogFailure $error) {
            return CatalogAdminRoutes::failure($request, $error);
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        }
    }
}
