<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use InvalidArgumentException;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use StudioSchedule\Domain\Date;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;
use StudioSchedule\Service\StatisticsService;

final readonly class StatisticsEndpoint
{
    public function __construct(private StatisticsService $service) {}

    public function __invoke(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        if (!in_array($principal->role, ['office', 'admin'], true)) {
            return Responses::problem($request, 403, 'forbidden');
        }

        $params = QueryParameters::read($request);
        $range = $params['range'] ?? '';
        $range = $range === '' ? 'last4Weeks' : $range;
        $errors = [];
        if (!in_array($range, ['last4Weeks', 'last12Weeks', 'term', 'custom'], true)) {
            $errors[] = ['location' => 'query.range', 'message' => 'expected one of last4Weeks, last12Weeks, term, custom'];
        }
        $dates = [];
        foreach (['from', 'to'] as $name) {
            try {
                $dates[$name] = Date::fromText($params[$name] ?? '');
            } catch (InvalidArgumentException) {
                $errors[] = ['location' => "query.$name", 'message' => 'expected a calendar date in YYYY-MM-DD form'];
            }
        }
        if ($errors !== []) {
            return Responses::problem($request, 422, 'validation failed', errors: $errors);
        }
        assert(isset($dates['from'], $dates['to']));
        if ($range === 'custom' && ($error = StatisticsService::customRangeError($dates['from'], $dates['to'])) !== null) {
            return Responses::problem($request, 422, 'validation failed: ' . $error);
        }

        try {
            $report = $this->service->report($range, $dates['from'], $dates['to']);
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        }

        return $report === null
            ? Responses::problem($request, 404, 'no term includes today')
            : Responses::json($response, $report, $request->getMethod() === 'HEAD')->withHeader('Cache-Control', 'no-store');
    }
}
