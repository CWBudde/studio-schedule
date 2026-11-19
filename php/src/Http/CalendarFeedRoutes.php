<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use DateTimeImmutable;
use DateTimeZone;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Slim\App;
use StudioSchedule\Domain\CalendarFeed;
use StudioSchedule\Domain\CalendarFeedProblem;
use StudioSchedule\Domain\CalendarFeedScope;
use StudioSchedule\Service\CalendarFeedManagement;
use StudioSchedule\Service\SessionPrincipal;

final readonly class CalendarFeedRoutes
{
    private const PATH = '/api/v1/my/calendar-feeds';
    private const ZERO_ID = '00000000-0000-0000-0000-000000000000';

    public function __construct(
        private CalendarFeedManagement $feeds,
        private string $basePath,
        private string $baseURL,
    ) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->get(self::PATH, $this->list(...));
        $app->post(self::PATH, $this->create(...));
        $app->delete(self::PATH . '/{feedId}', $this->revoke(...));
    }

    public function list(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }

        return Responses::json($response, ['items' => array_map(self::body(...), $this->feeds->list($principal))]);
    }

    public function create(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        $input = JsonBody::object($request, $response);
        if ($input instanceof ResponseInterface) {
            return $input;
        }

        $fields = get_object_vars($input);
        if (array_diff_key($fields, ['scope' => true, 'locationId' => true]) !== []) {
            return self::invalid($request, $response, 'body', 'unexpected property');
        }
        $rawScope = $fields['scope'] ?? null;
        $scope = is_string($rawScope) ? CalendarFeedScope::tryFrom($rawScope) : null;
        if ($scope === null) {
            $location = array_key_exists('scope', $fields) ? 'body.scope' : 'body';
            $message = array_key_exists('scope', $fields)
                ? (is_string($rawScope) ? 'expected value to be one of bookings, teaching, schedule' : 'expected string')
                : 'expected required property scope to be present';

            return self::invalid($request, $response, $location, $message);
        }

        $locationId = null;
        if (array_key_exists('locationId', $fields)) {
            $value = $fields['locationId'];
            if (!is_string($value) || !self::isId($value)) {
                return self::invalid($request, $response, 'body.locationId', 'expected UUID');
            }
            if ($scope !== CalendarFeedScope::Schedule) {
                return self::invalid($request, $response, 'body.locationId', 'only a schedule feed has a location');
            }
            $locationId = strtolower($value);
        }

        $result = $this->feeds->create($principal, $scope, $locationId);
        if ($result instanceof CalendarFeedProblem) {
            return match ($result) {
                CalendarFeedProblem::Forbidden => Responses::problem($request, 403, 'forbidden'),
                CalendarFeedProblem::NotFound => Responses::problem($request, 404, 'resource not found'),
                CalendarFeedProblem::UnknownLocation => self::invalid($request, $response, 'body.locationId', 'location does not exist'),
            };
        }

        $suffix = '/ical/' . $result['token'] . '.ics';
        $output = ['feed' => self::body($result['feed']), 'token' => $result['token'], 'path' => $this->basePath . $suffix];
        if ($this->baseURL !== '') {
            $output['url'] = $this->baseURL . $suffix;
        }

        return Responses::json($response->withStatus(201), $output);
    }

    /** @param array{feedId:string} $args */
    public function revoke(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        if (!self::isId($args['feedId'])) {
            return self::invalid($request, $response, 'path.feedId', 'expected UUID');
        }

        return $this->feeds->revoke($principal, strtolower($args['feedId']))
            ? $response->withStatus(204)
            : Responses::problem($request, 404, 'resource not found');
    }

    /** @return array<string,string> */
    private static function body(CalendarFeed $feed): array
    {
        $body = ['id' => $feed->id, 'scope' => $feed->scope->value];
        if ($feed->locationId !== null) {
            $body['locationId'] = $feed->locationId;
            if ($feed->locationName !== null && $feed->locationName !== '') {
                $body['locationName'] = $feed->locationName;
            }
        }
        $body['createdAt'] = self::timestamp($feed->createdAt);
        if ($feed->lastUsedAt !== null) {
            $body['lastUsedAt'] = self::timestamp($feed->lastUsedAt);
        }

        return $body;
    }

    private static function timestamp(DateTimeImmutable $time): string
    {
        $utc = $time->setTimezone(new DateTimeZone('UTC'));

        return rtrim(rtrim($utc->format('Y-m-d\TH:i:s.u'), '0'), '.') . 'Z';
    }

    private static function isId(string $value): bool
    {
        return $value !== self::ZERO_ID && preg_match('/\A[\da-f]{8}-[\da-f]{4}-[\da-f]{4}-[\da-f]{4}-[\da-f]{12}\z/i', $value) === 1;
    }

    private static function invalid(
        ServerRequestInterface $request,
        ResponseInterface $response,
        string $location,
        string $message,
    ): ResponseInterface {
        return Responses::validation($request, $response, [['location' => $location, 'message' => $message]]);
    }
}
