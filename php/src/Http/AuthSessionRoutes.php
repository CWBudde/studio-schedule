<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use DateTimeImmutable;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Log\LoggerInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Config;
use StudioSchedule\Service\AccountConfirmation;
use StudioSchedule\Service\ConfirmationFailure;
use StudioSchedule\Service\Impersonation;
use StudioSchedule\Service\ImpersonationFailure;
use StudioSchedule\Service\LoginFailure;
use StudioSchedule\Service\PasswordAuthentication;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SetupInput;
use StudioSchedule\Store\RateLimitStore;
use Throwable;

final readonly class AuthSessionRoutes
{
    private const int LOGIN_LIMIT = 20;
    private const int WINDOW_SECONDS = 900;

    public function __construct(
        private Config $config,
        private PasswordAuthentication $auth,
        private AccountConfirmation $confirmation,
        private Impersonation $impersonation,
        private MeEndpoint $profile,
        private RateLimitStore $limits,
        private PasswordFailureLimit $failures,
        private LoggerInterface $logger,
    ) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->post('/api/v1/auth/login', $this->login(...));
        $app->post('/api/v1/auth/logout', $this->logout(...));
        $app->post('/api/v1/auth/confirm', $this->confirm(...));
        $app->post('/api/v1/admin/accounts/{accountId}/impersonate', $this->impersonate(...));
        $app->post('/api/v1/auth/impersonation/stop', $this->stopImpersonation(...));
    }

    public function login(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $input = JsonBody::object($request, $response);
        if ($input instanceof ResponseInterface) {
            return $input;
        }

        $fields = get_object_vars($input);
        $errors = self::validate($fields);
        if ($errors !== []) {
            return Responses::validation($request, $response, $errors);
        }
        assert(is_string($fields['email']) && is_string($fields['password']));

        $peer = $request->getServerParams()['REMOTE_ADDR'] ?? '';
        $address = $this->config->trustedProxies->clientIP(is_string($peer) ? $peer : '', $request->getHeaderLine('X-Forwarded-For'));
        $retry = $this->limits->allow('login-ip', $address, self::LOGIN_LIMIT, self::WINDOW_SECONDS);
        if ($retry > 0) {
            return Responses::tooManyAttempts($request, $retry);
        }

        $email = SetupInput::trim($fields['email']);
        $attempt = $this->failures->reserve($email);
        if (is_int($attempt)) {
            return Responses::tooManyAttempts($request, $attempt);
        }
        try {
            $result = $this->auth->signIn($email, $fields['password']);
            if ($result === LoginFailure::InvalidCredentials) {
                $attempt->count();

                return Responses::problem($request, 401, 'invalid email or password');
            }
            if ($result === LoginFailure::EmailNotConfirmed) {
                return Responses::problem($request, 403, 'email address not confirmed');
            }

            $attempt->clearFailures();

            return $this->replaceSession($request, $response, $result);
        } finally {
            $attempt->release();
        }
    }

    public function confirm(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $input = JsonBody::strings($request, $response, ['token' => [1, 100], 'password' => [1, 256]]);
        if ($input instanceof ResponseInterface) {
            return $input;
        }

        $result = $this->confirmation->confirm($input['token'], $input['password']);

        return match ($result) {
            ConfirmationFailure::InvalidToken => Responses::problem($request, 410, 'the link is invalid or has expired'),
            ConfirmationFailure::InvalidCredentials => Responses::problem($request, 401, 'invalid email or password'),
            ConfirmationFailure::AccountNotFound => Responses::problem($request, 404, 'resource not found'),
            default => $this->replaceSession($request, $response, $result),
        };
    }

    /** @param array{accountId:string} $args */
    public function impersonate(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        array $args,
    ): ResponseInterface {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        if ($principal->role !== 'admin') {
            return Responses::problem($request, 403, 'forbidden');
        }

        $id = $args['accountId'];
        if ($id === '00000000-0000-0000-0000-000000000000' || preg_match('/\A[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\z/i', $id) !== 1) {
            return Responses::validation($request, $response, [['location' => 'path.accountId', 'message' => 'expected an account ID']]);
        }

        return $this->impersonationResponse($request, $response, $this->impersonation->start($principal, self::token($request), strtolower($id)));
    }

    public function stopImpersonation(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }

        return $this->impersonationResponse($request, $response, $this->impersonation->stop($principal, self::token($request)));
    }

    /** @param array{principal:SessionPrincipal,token:string,expiresAt:DateTimeImmutable}|ImpersonationFailure $result */
    private function impersonationResponse(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        #[SensitiveParameter]
        array|ImpersonationFailure $result,
    ): ResponseInterface {
        return match ($result) {
            ImpersonationFailure::NotImpersonable => Responses::problem($request, 403, 'this account cannot be impersonated'),
            ImpersonationFailure::AccountInactive => Responses::problem($request, 409, 'the account is disabled or not confirmed'),
            ImpersonationFailure::NotImpersonating => Responses::problem($request, 409, 'not signed in as another account'),
            ImpersonationFailure::NoSession => Responses::problem($request, 401, 'not signed in'),
            ImpersonationFailure::AccountNotFound => Responses::problem($request, 404, 'resource not found'),
            default => $this->signedIn($request, $response, $result),
        };
    }

    /** @param array{principal:SessionPrincipal,token:string,expiresAt:DateTimeImmutable} $result */
    private function replaceSession(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        #[SensitiveParameter]
        array $result,
    ): ResponseInterface {
        try {
            $this->auth->signOut(self::token($request));
        } catch (Throwable $error) {
            $this->logger->warning('end previous session', ['requestId' => $request->getAttribute('requestId'), 'exception' => $error::class]);
        }

        return $this->signedIn($request, $response, $result);
    }

    /** @param array{principal:SessionPrincipal,token:string,expiresAt:DateTimeImmutable} $result */
    private function signedIn(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        #[SensitiveParameter]
        array $result,
    ): ResponseInterface {
        $profile = $this->profile->signedIn($request, $response, $result['principal']);
        if ($profile->getStatusCode() !== 200) {
            return $profile;
        }

        $cookie = (new SessionCookies($this->config))->issue($result['token'], $result['expiresAt']);

        return $profile->withAddedHeader('Set-Cookie', $cookie);
    }

    public function logout(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $this->auth->signOut(self::token($request));
        $cookie = (new SessionCookies($this->config))->expire($this->config->cookieName());

        return $response->withStatus(204)->withAddedHeader('Set-Cookie', $cookie);
    }

    private static function token(#[SensitiveParameter] ServerRequestInterface $request): string
    {
        $token = $request->getAttribute('sessionToken');

        return is_string($token) ? $token : '';
    }

    /**
     * @param array<array-key,mixed> $fields
     * @return list<array{location:string,message:string}>
     */
    private static function validate(#[SensitiveParameter] array $fields): array
    {
        $lengths = ['email' => 254, 'password' => 256];
        $errors = [];
        foreach ($lengths as $name => $limit) {
            if (!array_key_exists($name, $fields)) {
                $errors[] = ['location' => 'body', 'message' => "expected required property $name to be present"];
                continue;
            }
            $value = $fields[$name];
            $location = 'body.' . $name;
            if (!is_string($value)) {
                $errors[] = ['location' => $location, 'message' => 'expected string'];
                continue;
            }
            $length = mb_strlen($value, 'UTF-8');
            if ($length === 0 || $length > $limit) {
                $errors[] = [
                    'location' => $location,
                    'message' => $length === 0 ? 'expected length >= 1' : "expected length <= $limit",
                ];
            }
        }
        if (array_diff_key($fields, $lengths) !== []) {
            $errors[] = ['location' => 'body', 'message' => 'unexpected property'];
        }

        return $errors;
    }
}
