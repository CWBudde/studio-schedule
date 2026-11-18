<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Store\SessionStore;

final readonly class MeEndpoint
{
    public function __construct(private SessionStore $store) {}

    public function __invoke(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }

        $profile = $this->store->profile($principal->accountId);
        if ($profile === null) {
            return Responses::problem($request, 404, 'not found');
        }
        if ($profile['locale'] === null || $profile['locale'] === '') {
            unset($profile['locale']);
        }
        if (in_array($profile['role'], ['office', 'admin'], true)) {
            $profile['version'] = 'dev';
        }
        if ($principal->impersonatorId !== null) {
            $admin = $this->store->profile($principal->impersonatorId);
            if ($admin === null) {
                return Responses::problem($request, 404, 'not found');
            }
            $profile['impersonator'] = [
                'accountId' => $admin['accountId'],
                'email' => $admin['email'],
                'givenName' => $admin['givenName'],
                'familyName' => $admin['familyName'],
            ];
        }

        return Responses::json($response, $profile, $request->getMethod() === 'HEAD');
    }
}
