<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\LessonSignup;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;

final readonly class LessonSignupRoutes
{
    public function __construct(private LessonSignup $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->post('/api/v1/lessons/{lessonId}/{action:bookings|waitlist|participants}', $this->signup(...));
    }

    /** @param array{lessonId:string,action:'bookings'|'waitlist'|'participants'} $args */
    private function signup(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        $staff = $args['action'] === 'participants';
        if ($staff && !in_array($principal->role, ['instructor', 'office', 'admin'], true)) {
            return Responses::problem($request, 403, 'forbidden');
        }

        try {
            $body = CatalogAdminRoutes::fields($request, match ($args['action']) {
                'bookings' => ['personid'],
                'waitlist' => ['personid', 'stayonlist'],
                'participants' => ['personid', 'guest', 'trial', 'trialnote', 'confirmovercapacity'],
            });
            if ($body instanceof ResponseInterface) {
                return $body;
            }
            $id = $args['lessonId'];
            if (!Id::valid($id) || stripos($request->getUri()->getPath(), '%2f') !== false) {
                throw new CatalogFailure(422, 'invalid ID', 'path.lessonId');
            }
            $lesson = strtolower($id);

            if ($staff) {
                $this->service->rejectStaffSession($lesson, self::participant($body), $principal);

                return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', 'GET');
            }
            if ($args['action'] === 'waitlist') {
                CatalogAdminRoutes::flag($body, 'stayOnList');
            }
            $this->service->rejectMemberSession($lesson, CatalogAdminRoutes::requiredId($body, 'personId'), $principal);

            return Responses::problem($request, 404, 'resource not found');
        } catch (CatalogFailure $failure) {
            return CatalogAdminRoutes::failure($request, $failure);
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        }
    }

    /** @param array<array-key,mixed> $body */
    private static function participant(array $body): ?string
    {
        $person = CatalogAdminRoutes::optionalId($body, 'personId');
        $guest = $body['guest'] ?? null;
        if (($person === null) === ($guest === null)) {
            throw new CatalogFailure(422, 'give either a person or a guest', 'body');
        }
        // Validate the request shape before the service checks whether the lesson accepts bookings.
        if ($guest !== null) {
            GuestInput::read($guest, allowNul: true);
        }
        CatalogAdminRoutes::flag($body, 'trial');
        CatalogAdminRoutes::text($body, 'trialNote', 500, allowNul: true);
        CatalogAdminRoutes::flag($body, 'confirmOverCapacity');

        return $person;
    }
}
