<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\AccountInvitation;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\SessionPrincipal;

final readonly class AccountInvitationEndpoint
{
    public function __construct(private AccountInvitation $invitations) {}

    /** @param array{personId:string} $args */
    public function __invoke(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $person = $args['personId'];
        if (!Id::valid($person)) {
            throw new CatalogFailure(422, 'invalid ID', 'path.personId');
        }
        $body = CatalogAdminRoutes::fields($request, ['email', 'role']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $email = JsonBody::email(CatalogAdminRoutes::text($body, 'email', 254, required: true));
        if ($email === null) {
            throw CatalogFailure::field('email', 'expected RFC 5322 email');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);
        $account = $this->invitations->create(
            $principal,
            strtolower($person),
            $email,
            CatalogAdminRoutes::text($body, 'role', 32, required: true),
        );

        return Responses::json($response->withStatus(201), $account)->withHeader('Cache-Control', 'no-store');
    }
}
