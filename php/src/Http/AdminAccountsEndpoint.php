<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Store\AdminAccountsStore;

final readonly class AdminAccountsEndpoint
{
    public function __construct(private AdminAccountsStore $accounts) {}

    public function __invoke(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        if ($principal->role !== 'admin' || $principal->impersonatorId !== null) {
            return Responses::problem($request, 403, 'forbidden');
        }

        return Responses::json($response, ['items' => $this->accounts->list()], $request->getMethod() === 'HEAD');
    }
}
