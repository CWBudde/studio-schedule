<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
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
        $app->post('/api/v1/bookings/{bookingId}/cancel', $this->cancel(...));
    }

    /** @param array{bookingId:string} $args */
    private function cancel(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }

        try {
            $id = $args['bookingId'];
            if (!Id::valid($id) || stripos($request->getUri()->getPath(), '%2f') !== false) {
                throw new CatalogFailure(422, 'invalid ID', 'path.bookingId');
            }

            return Responses::json($response, $this->cancellations->cancel($principal, strtolower($id)))
                ->withHeader('Cache-Control', 'no-store');
        } catch (CatalogFailure $failure) {
            return CatalogAdminRoutes::failure($request, $failure);
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        }
    }
}
