<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Closure;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Http\Server\RequestHandlerInterface;
use SensitiveParameter;
use Slim\App;
use stdClass;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\EventCancellations;
use StudioSchedule\Service\EventPayments;
use StudioSchedule\Service\EventRegistrationReads;
use StudioSchedule\Service\EventRegistrationWrites;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;
use StudioSchedule\Service\SetupInput;

final readonly class EventRegistrationRoutes
{
    /** @param Closure():EventRegistrationWrites $writes */
    public function __construct(
        private EventRegistrationReads $registrations,
        private Closure $writes,
        private EventPayments $payments,
        private EventCancellations $cancellations,
    ) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->get('/api/v1/admin/events/{eventId}/registrations', $this->office(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->get('/api/v1/my/events', $this->mine(...));
        $app->post('/api/v1/admin/events/{eventId}/registrations', $this->desk(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->post('/api/v1/admin/event-registrations/{registrationId}/pay', $this->pay(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->post('/api/v1/admin/event-registrations/{registrationId}/cancel', $this->cancelAtDesk(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->post('/api/v1/event-registrations/{registrationId}/cancel', $this->cancelOwn(...))->add(self::requireSignedIn(...));
        $app->post('/api/v1/events/{slug}/{action:registrations|waitlist}', $this->signup(...))->add(self::requireSignedIn(...));
        $app->post('/api/v1/event-registrations/{registrationId}/{action:accept|decline|leave}', $this->respond(...))
            ->add(self::requireSignedIn(...));
    }

    /** @param array{eventId:string} $args */
    private function office(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $items = $this->registrations->office(self::pathId($request, $args['eventId'], 'eventId'));

        return $items === null ? Responses::problem($request, 404, 'resource not found')
            : Responses::json($response, ['items' => $items])->withHeader('Cache-Control', 'no-store');
    }

    /** @param array{eventId:string} $args */
    private function desk(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = self::pathId($request, $args['eventId'], 'eventId');
        $body = CatalogAdminRoutes::fields($request, ['personid', 'guest', 'confirmovercapacity']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $person = CatalogAdminRoutes::optionalId($body, 'personId');
        $guest = $body['guest'] ?? null;
        if (($person === null) === ($guest === null)) {
            throw new CatalogFailure(422, 'give either a person or a guest', 'body');
        }
        $confirm = CatalogAdminRoutes::flag($body, 'confirmOverCapacity');

        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);
        $registration = ($this->writes)()->desk($principal, $id, $person, $guest === null ? null : self::guest($guest), $confirm);

        return Responses::json($response->withStatus(201), $registration);
    }

    /** @param array{registrationId:string} $args */
    private function pay(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = self::pathId($request, $args['registrationId'], 'registrationId');
        $body = CatalogAdminRoutes::fields($request, ['paymentmethod', 'payerid', 'recipientname', 'recipientaddress', 'recipientemail']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $method = CatalogAdminRoutes::text($body, 'paymentMethod', 8) ?? 'cash';
        if (!in_array($method, ['cash', 'transfer', 'card'], true)) {
            throw CatalogFailure::field('paymentMethod', 'expected cash, transfer or card');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);

        return Responses::json($response, $this->payments->pay($principal, $id, [
            'method' => $method,
            'payerId' => CatalogAdminRoutes::optionalId($body, 'payerId'),
            'name' => SetupInput::trim(CatalogAdminRoutes::text($body, 'recipientName', 200) ?? ''),
            'address' => SetupInput::trim(CatalogAdminRoutes::text($body, 'recipientAddress', 500) ?? ''),
            'email' => SetupInput::trim(CatalogAdminRoutes::text($body, 'recipientEmail', 254) ?? ''),
        ]))->withHeader('Cache-Control', 'no-store');
    }

    /** @param array{registrationId:string} $args */
    private function cancelAtDesk(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = self::pathId($request, $args['registrationId'], 'registrationId');
        $body = CatalogAdminRoutes::fields($request, ['refund']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);

        return Responses::json($response, $this->cancellations->office($principal, $id, CatalogAdminRoutes::flag($body, 'refund')));
    }

    /** @param array{registrationId:string} $args */
    private function cancelOwn(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = self::pathId($request, $args['registrationId'], 'registrationId');
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);

        return Responses::json($response, $this->cancellations->member($principal, $id));
    }

    /** @return array{name:string,email:string} */
    private static function guest(#[SensitiveParameter] mixed $input): array
    {
        if (!$input instanceof stdClass) {
            throw CatalogFailure::field('guest', 'expected object');
        }
        $fields = [];
        foreach (get_object_vars($input) as $key => $value) {
            $field = strtolower((string) $key);
            if (!in_array($field, ['name', 'email'], true)) {
                throw CatalogFailure::field("guest.$key", 'unexpected property');
            }
            $fields[$field] = $value;
        }
        try {
            return [
                'name' => SetupInput::trim(CatalogAdminRoutes::text($fields, 'name', 200, true)),
                'email' => SetupInput::trim(CatalogAdminRoutes::text($fields, 'email', 254) ?? ''),
            ];
        } catch (CatalogFailure $failure) {
            $field = $failure->location === 'body' ? 'guest' : 'guest.' . substr($failure->location, strlen('body.'));
            throw CatalogFailure::field($field, $failure->getMessage());
        }
    }

    /** @param array{slug:string,action:'registrations'|'waitlist'} $args */
    private function signup(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $slug = $args['slug'];
        $problem = EventRoutes::slugProblem($request, $slug);
        if ($problem !== null) {
            return $problem;
        }
        $body = CatalogAdminRoutes::fields($request, ['personid']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);
        $result = ($this->writes)()->signup(
            $principal,
            $slug,
            CatalogAdminRoutes::requiredId($body, 'personId'),
            $args['action'] === 'waitlist',
        );

        return Responses::json($response->withStatus(201), $result);
    }

    /** @param array{registrationId:string,action:'accept'|'decline'|'leave'} $args */
    private function respond(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);
        $id = self::pathId($request, $args['registrationId'], 'registrationId');

        return Responses::json($response, ($this->writes)()->respond($principal, $id, $args['action']));
    }

    private static function pathId(ServerRequestInterface $request, string $id, string $field): string
    {
        if (!Id::valid($id) || stripos($request->getUri()->getPath(), '%2f') !== false) {
            throw new CatalogFailure(422, 'invalid ID', "path.$field");
        }

        return strtolower($id);
    }

    private static function requireSignedIn(#[SensitiveParameter] ServerRequestInterface $request, RequestHandlerInterface $next): ResponseInterface
    {
        if (!$request->getAttribute('sessionPrincipal') instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }

        try {
            return $next->handle($request);
        } catch (CatalogFailure $failure) {
            return CatalogAdminRoutes::failure($request, $failure);
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        }
    }

    private function mine(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        if ($request->getMethod() === 'HEAD') {
            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', 'GET');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }

        return Responses::json($response, ['items' => $this->registrations->mine($principal->personId)])
            ->withHeader('Cache-Control', 'no-store');
    }
}
