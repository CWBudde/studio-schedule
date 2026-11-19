<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use StudioSchedule\Service\AccountRegistration;
use StudioSchedule\Service\CommonPasswords;
use StudioSchedule\Service\InvalidSetupInput;

final readonly class AccountRegistrationEndpoint
{
    public function __construct(
        private AccountRegistration $registration,
        private AuthMailLimit $mailLimit,
        private string $baseURL,
    ) {}

    public function __invoke(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $input = JsonBody::strings($request, $response, [
            'givenName' => [1, 200],
            'familyName' => [1, 200],
            'email' => [1, 254],
            'password' => [CommonPasswords::MIN_LENGTH, 256],
            'locale' => [0, 35],
        ], ['locale']);
        if ($input instanceof ResponseInterface) {
            return $input;
        }
        $email = JsonBody::email($input['email']);
        if ($email === null) {
            return Responses::validation($request, $response, [['location' => 'body.email', 'message' => 'expected RFC 5322 email']]);
        }
        if ($this->baseURL === '') {
            return Responses::problem($request, 503, 'registration is unavailable');
        }
        $retry = $this->mailLimit->retryAfter($request);
        if ($retry > 0) {
            return Responses::tooManyAttempts($request, $retry);
        }

        try {
            $this->registration->register($input['givenName'], $input['familyName'], $email, $input['password'], $input['locale'] ?? '');
        } catch (InvalidSetupInput $error) {
            return Responses::validation($request, $response, [['location' => 'body.' . $error->field, 'message' => $error->getMessage()]]);
        }

        return $response->withStatus(202);
    }
}
