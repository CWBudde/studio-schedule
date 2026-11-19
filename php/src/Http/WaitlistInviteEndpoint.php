<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use StudioSchedule\Service\SettingsNotFound;
use StudioSchedule\Service\WaitlistInvites;
use StudioSchedule\Store\RateLimitStore;

final readonly class WaitlistInviteEndpoint
{
    public function __construct(
        private WaitlistInvites $invites,
        private TrustedProxies $proxies,
        private RateLimitStore $limits,
    ) {}

    /** @param array{token:string} $args */
    public function __invoke(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        #[SensitiveParameter]
        array $args,
    ): ResponseInterface {
        if ($request->getMethod() === 'HEAD') {
            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', 'GET');
        }
        $token = $args['token'];
        if ($token === '' || mb_strlen($token, 'UTF-8') > 128) {
            return Responses::problem($request, 422, 'validation failed', errors: [
                ['location' => 'path.token', 'message' => 'expected a token of 1 to 128 characters'],
            ]);
        }

        $peer = $request->getServerParams()['REMOTE_ADDR'] ?? '';
        $address = $this->proxies->clientIP(is_string($peer) ? $peer : '', $request->getHeaderLine('X-Forwarded-For'));
        $retry = $this->limits->allow('waitlist-invite-ip', $address, 20, 900);
        if ($retry > 0) {
            return Responses::tooManyAttempts($request, $retry);
        }
        try {
            $document = $this->invites->view($token);
        } catch (SettingsNotFound) {
            $document = null;
        }

        return $document === null ? Responses::problem($request, 404, 'resource not found')
            : Responses::json($response, $document)->withHeader('Cache-Control', 'no-store');
    }
}
