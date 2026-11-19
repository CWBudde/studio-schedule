<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use StudioSchedule\Config;
use StudioSchedule\Service\PasswordResetRequests;

final readonly class PasswordResetRequestEndpoint
{
    public function __construct(
        private Config $config,
        private PasswordResetRequests $resets,
        private AuthMailLimit $mailLimit,
    ) {}

    public function __invoke(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $input = JsonBody::strings($request, $response, ['email' => [1, 254], 'locale' => [0, 35]], ['locale']);
        if ($input instanceof ResponseInterface) {
            return $input;
        }

        if ($this->config->baseURL === '') {
            // The mail needs a trusted absolute URL. Fail before looking up the account.
            return Responses::problem($request, 503, 'password recovery is unavailable');
        }

        $retry = $this->mailLimit->retryAfter($request);
        if ($retry > 0) {
            return Responses::tooManyAttempts($request, $retry);
        }

        $this->resets->request($input['email'], $input['locale'] ?? '');

        return $response->withStatus(202);
    }
}
