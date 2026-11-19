<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Closure;
use InvalidArgumentException;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\BookingCancellation;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;

final readonly class BookingRoutes
{
    public function __construct(private BookingCancellation $cancellations) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->post('/api/v1/bookings/{bookingId}/cancel', $this->handler(following: false));
        $app->map(['GET', 'POST'], '/api/v1/bookings/{bookingId}/cancel-following', $this->handler(following: true));
    }

    /** @return Closure(ServerRequestInterface, ResponseInterface, array{bookingId:string}):ResponseInterface */
    private function handler(bool $following): Closure
    {
        return function (#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args) use ($following): ResponseInterface {
            $principal = $request->getAttribute('sessionPrincipal');
            if (!$principal instanceof SessionPrincipal) {
                return Responses::problem($request, 401, 'not signed in');
            }

            try {
                $id = $args['bookingId'] ?? null;
                if (!is_string($id) || !Id::valid($id) || stripos($request->getUri()->getPath(), '%2f') !== false) {
                    throw new CatalogFailure(422, 'invalid ID', 'path.bookingId');
                }

                $id = strtolower($id);
                if ($following) {
                    $apply = $request->getMethod() === 'POST';
                    if ($apply) {
                        $body = CatalogAdminRoutes::fields($request, ['until'], optional: true);
                        if ($body instanceof ResponseInterface) {
                            return $body;
                        }
                        $until = self::date($body['until'] ?? null, 'body.until');
                    } else {
                        $query = QueryParameters::read($request);
                        $until = self::date(($query['until'] ?? '') === '' ? null : $query['until'], 'query.until');
                    }
                    $document = $this->cancellations->following($principal, $id, $until, $apply);
                } else {
                    $document = $this->cancellations->cancel($principal, $id);
                }

                return Responses::json($response, $document)->withHeader('Cache-Control', 'no-store');
            } catch (CatalogFailure $failure) {
                return CatalogAdminRoutes::failure($request, $failure);
            } catch (SettingsNotFound) {
                return Responses::problem($request, 404, 'resource not found');
            }
        };
    }

    private static function date(mixed $value, string $field): ?Date
    {
        if ($value === null) {
            return null;
        }
        if (is_string($value)) {
            try {
                return Date::parse($value);
            } catch (InvalidArgumentException) {
                throw new CatalogFailure(422, 'invalid date', $field);
            }
        }

        throw new CatalogFailure(422, 'expected a date', $field);
    }
}
