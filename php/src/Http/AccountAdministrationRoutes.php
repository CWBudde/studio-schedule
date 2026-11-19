<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Http\Server\RequestHandlerInterface;
use SensitiveParameter;
use Slim\App;
use Slim\Interfaces\RouteCollectorProxyInterface;
use StudioSchedule\Config;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\AccountAdministration;
use StudioSchedule\Service\AccountAdministrationFailure;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Store\SessionStore;

final readonly class AccountAdministrationRoutes
{
    public function __construct(
        private AccountAdministration $accounts,
        private SessionStore $sessions,
        private PasswordFailureLimit $failures,
        private Config $config,
    ) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->group('/api/v1/admin/accounts/{accountId}', function (RouteCollectorProxyInterface $routes): void {
            foreach (['disable', 'enable'] as $action) {
                $routes->post('/' . $action, function (ServerRequestInterface $request, ResponseInterface $response, array $args) use ($action): ResponseInterface {
                    $id = $args['accountId'] ?? null;

                    return $this->setDisabled($request, $response, is_string($id) ? $id : '', $action === 'disable');
                });
            }
            $routes->put('/role', $this->setRole(...));
            $routes->delete('', $this->delete(...));
        })->add(self::requireAdmin(...));
        $app->delete('/api/v1/my/account', $this->deleteOwn(...));
    }

    private static function requireAdmin(ServerRequestInterface $request, RequestHandlerInterface $next): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        if ($principal->role !== 'admin' || $principal->impersonatorId !== null) {
            return Responses::problem($request, 403, 'forbidden');
        }

        return $next->handle($request);
    }

    private function setDisabled(ServerRequestInterface $request, ResponseInterface $response, string $accountId, bool $disabled): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);
        $id = self::accountId($request, $response, $accountId);
        if ($id instanceof ResponseInterface) {
            return $id;
        }

        return self::result($request, $response, $this->accounts->setDisabled($principal, $id, $disabled));
    }

    /** @param array{accountId:string} $args */
    public function setRole(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);
        $id = self::accountId($request, $response, $args['accountId']);
        if ($id instanceof ResponseInterface) {
            return $id;
        }
        $input = JsonBody::strings($request, $response, ['role' => [1, 10]]);
        if ($input instanceof ResponseInterface) {
            return $input;
        }
        $role = $input['role'];
        if (!in_array($role, ['member', 'instructor', 'office', 'admin'], true)) {
            return Responses::validation($request, $response, [['location' => 'body.role', 'message' => 'invalid account role']]);
        }

        return self::result($request, $response, $this->accounts->setRole($principal, $id, $role));
    }

    /** @param array{accountId:string} $args */
    public function delete(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);
        $id = self::accountId($request, $response, $args['accountId']);
        if ($id instanceof ResponseInterface) {
            return $id;
        }

        return self::result($request, $response, $this->accounts->delete($principal, $id));
    }

    public function deleteOwn(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        $input = JsonBody::strings($request, $response, ['password' => [1, 256], 'locale' => [0, 35]], ['locale']);
        if ($input instanceof ResponseInterface) {
            return $input;
        }
        $profile = $this->sessions->profile($principal->accountId);
        if ($profile === null) {
            return Responses::problem($request, 401, 'not signed in');
        }
        $attempt = $this->failures->reserve($profile['email']);
        if (is_int($attempt)) {
            return Responses::tooManyAttempts($request, $attempt);
        }

        try {
            $failure = $this->accounts->deleteOwn($principal, $input['password'], $input['locale'] ?? '');
            if ($failure === AccountAdministrationFailure::WrongPassword) {
                $attempt->count();
            }
            if ($failure !== null) {
                return self::result($request, $response, $failure);
            }

            $cookie = (new SessionCookies($this->config))->expire($this->config->cookieName());

            return $response->withStatus(204)->withHeader('Set-Cookie', $cookie);
        } finally {
            $attempt->release();
        }
    }

    private static function accountId(ServerRequestInterface $request, ResponseInterface $response, string $id): string|ResponseInterface
    {
        return Id::valid($id) ? strtolower($id) : Responses::validation($request, $response, [['location' => 'path.accountId', 'message' => 'invalid ID']]);
    }

    /** @param array<string, mixed>|AccountAdministrationFailure|null $result */
    private static function result(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        array|AccountAdministrationFailure|null $result,
    ): ResponseInterface {
        if (is_array($result)) {
            return Responses::json($response, $result);
        }

        return match ($result) {
            null => $response->withStatus(204),
            AccountAdministrationFailure::NotFound => Responses::problem($request, 404, 'resource not found'),
            AccountAdministrationFailure::OwnAccount => Responses::problem($request, 409, 'another admin changes this account', reason: 'own-account'),
            AccountAdministrationFailure::LastAdmin => Responses::problem($request, 409, 'the school needs at least one admin account that can sign in', reason: 'last-admin'),
            AccountAdministrationFailure::InUse => Responses::problem($request, 409, 'receipts, the audit log, announcements or duplicate dismissals name the account; disable it instead', reason: 'in-use'),
            AccountAdministrationFailure::Impersonating => Responses::problem($request, 403, 'not while signed in as another account'),
            AccountAdministrationFailure::WrongPassword => Responses::validation($request, $response, [['location' => 'body.password', 'message' => 'the password is wrong']]),
        };
    }
}
