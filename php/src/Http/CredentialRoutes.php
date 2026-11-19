<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Closure;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Service\CommonPasswords;
use StudioSchedule\Service\EmailChange;
use StudioSchedule\Service\EmailChangeFailure;
use StudioSchedule\Service\PasswordChange;
use StudioSchedule\Service\PasswordChangeFailure;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Store\SessionStore;

final readonly class CredentialRoutes
{
    public function __construct(
        private PasswordChange $passwordChange,
        private EmailChange $emailChange,
        private SessionStore $sessions,
        private PasswordFailureLimit $failures,
        private AuthMailLimit $mailLimit,
        private string $baseURL,
    ) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->post('/api/v1/auth/password', $this->changePassword(...));
        $app->post('/api/v1/auth/email', $this->requestEmail(...));
        $app->post('/api/v1/auth/email/confirm', $this->confirmEmail(...));
    }

    public function changePassword(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        $input = JsonBody::strings($request, $response, [
            'currentPassword' => [1, 256],
            'newPassword' => [CommonPasswords::MIN_LENGTH, 256],
        ]);
        if ($input instanceof ResponseInterface) {
            return $input;
        }

        return $this->checkingPassword(
            $request,
            $response,
            $principal,
            'body.currentPassword',
            204,
            fn(string $token): ?PasswordChangeFailure => $this->passwordChange->change($principal, $token, $input['currentPassword'], $input['newPassword']),
        );
    }

    public function requestEmail(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        $input = JsonBody::strings($request, $response, ['newEmail' => [1, 254], 'password' => [1, 256], 'locale' => [0, 35]], ['locale']);
        if ($input instanceof ResponseInterface) {
            return $input;
        }
        $email = JsonBody::email($input['newEmail']);
        if ($email === null) {
            return self::validation($request, $response, 'body.newEmail', 'expected RFC 5322 email');
        }
        if ($this->baseURL === '') {
            return Responses::problem($request, 503, 'email change is unavailable');
        }
        $mail = $this->mailLimit->reserve($request);
        if (is_int($mail)) {
            return Responses::tooManyAttempts($request, $mail);
        }

        try {
            $result = $this->checkingPassword(
                $request,
                $response,
                $principal,
                'body.password',
                202,
                fn(string $token): ?EmailChangeFailure => $this->emailChange->request($principal, $token, $input['password'], $email, $input['locale'] ?? ''),
            );
            if ($result->getStatusCode() === 202) {
                $mail->count();
            }

            return $result;
        } finally {
            $mail->release();
        }
    }

    public function confirmEmail(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $input = JsonBody::strings($request, $response, ['token' => [1, 100], 'locale' => [0, 35]], ['locale']);
        if ($input instanceof ResponseInterface) {
            return $input;
        }

        return self::outcome($request, $response, $this->emailChange->confirm($input['token'], $input['locale'] ?? ''), 204);
    }

    /** @param Closure(string): (PasswordChangeFailure|EmailChangeFailure|null) $operation */
    private function checkingPassword(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        SessionPrincipal $principal,
        string $location,
        int $success,
        #[SensitiveParameter]
        Closure $operation,
    ): ResponseInterface {
        $token = $request->getAttribute('sessionToken');
        $profile = $this->sessions->profile($principal->accountId);
        if (!is_string($token) || $profile === null) {
            return Responses::problem($request, 401, 'not signed in');
        }

        $attempt = $this->failures->reserve($profile['email']);
        if (is_int($attempt)) {
            return Responses::tooManyAttempts($request, $attempt);
        }
        try {
            $result = $operation($token);
            if ($result === PasswordChangeFailure::WrongPassword || $result === EmailChangeFailure::WrongPassword) {
                $attempt->count();

                return self::validation($request, $response, $location, 'the password is wrong');
            }

            return self::outcome($request, $response, $result, $success);
        } finally {
            $attempt->release();
        }
    }

    private static function outcome(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        PasswordChangeFailure|EmailChangeFailure|null $result,
        int $success,
    ): ResponseInterface {
        return match ($result) {
            null => $response->withStatus($success),
            PasswordChangeFailure::CommonPassword => self::validation($request, $response, 'body.newPassword', 'the password is one of the most common ones; choose another'),
            EmailChangeFailure::SameEmail => self::validation($request, $response, 'body.newEmail', "this is the account's email address already"),
            EmailChangeFailure::InvalidEmail => Responses::problem($request, 422, 'validation failed'),
            PasswordChangeFailure::Impersonating, EmailChangeFailure::Impersonating => Responses::problem($request, 403, 'not while signed in as another account'),
            PasswordChangeFailure::NoSession, EmailChangeFailure::NoSession => Responses::problem($request, 401, 'not signed in'),
            EmailChangeFailure::InvalidToken => Responses::problem($request, 410, 'the link is invalid or has expired'),
            EmailChangeFailure::EmailTaken => Responses::problem($request, 409, 'the email address belongs to another account'),
            PasswordChangeFailure::WrongPassword, EmailChangeFailure::WrongPassword => throw new \LogicException('Password failures require a field name.'),
        };
    }

    private static function validation(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        string $location,
        string $message,
    ): ResponseInterface {
        return Responses::validation($request, $response, [['location' => $location, 'message' => $message]]);
    }
}
