<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\PassIssuance;
use StudioSchedule\Service\PassVoiding;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SetupInput;

/** @phpstan-import-type Purchase from PassIssuance */
final readonly class PassIssueRoutes
{
    public function __construct(private PassIssuance $passes, private PassVoiding $voiding) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->post('/api/v1/admin/persons/{personId}/passes/{action:assign|sell}', $this->issue(...))
            ->add(CatalogAdminRoutes::requireOffice(...));
        $app->post('/api/v1/admin/passes/{passId}/void', $this->void(...))->add(CatalogAdminRoutes::requireOffice(...));
    }

    /** @param array{passId:string} $args */
    private function void(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = $args['passId'];
        if (!Id::valid($id) || stripos($request->getUri()->getPath(), '%2f') !== false) {
            throw new CatalogFailure(422, 'invalid ID', 'path.passId');
        }

        return Responses::json($response, $this->voiding->void(strtolower($id)))->withHeader('Cache-Control', 'no-store');
    }

    /** @param array{personId:string,action:'assign'|'sell'} $args */
    private function issue(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $person = $args['personId'];
        if (!Id::valid($person) || stripos($request->getUri()->getPath(), '%2f') !== false) {
            throw new CatalogFailure(422, 'invalid ID', 'path.personId');
        }
        $selling = $args['action'] === 'sell';
        $allowed = ['passtypeid', 'validfrom', 'note'];
        if ($selling) {
            $allowed = [...$allowed, 'payerid', 'price', 'paymentmethod', 'recipientname', 'recipientaddress', 'recipientemail'];
        }
        $body = CatalogAdminRoutes::fields($request, $allowed);
        if ($body instanceof ResponseInterface) {
            return $body;
        }

        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);
        $from = isset($body['validfrom']) ? CatalogAdminRoutes::date($body, 'validFrom') : null;
        $pass = $this->passes->issue(
            $principal,
            strtolower($person),
            CatalogAdminRoutes::requiredId($body, 'passTypeId'),
            $from,
            SetupInput::trim(CatalogAdminRoutes::text($body, 'note', 500) ?? ''),
            $selling ? self::purchase($body) : null,
        );

        return Responses::json($response->withStatus(201), $pass)->withHeader('Cache-Control', 'no-store');
    }

    /**
     * @param array<array-key,mixed> $body
     * @return Purchase
     */
    private static function purchase(#[SensitiveParameter] array $body): array
    {
        $method = CatalogAdminRoutes::text($body, 'paymentMethod', 8) ?? 'cash';
        if (!in_array($method, ['cash', 'transfer', 'card'], true)) {
            throw CatalogFailure::field('paymentMethod', 'expected cash, transfer or card');
        }

        return [
            'method' => $method, 'payerId' => CatalogAdminRoutes::optionalId($body, 'payerId'),
            'price' => CatalogAdminRoutes::number($body, 'price', 0, 100_000_000),
            'name' => SetupInput::trim(CatalogAdminRoutes::text($body, 'recipientName', 200) ?? ''),
            'address' => SetupInput::trim(CatalogAdminRoutes::text($body, 'recipientAddress', 500) ?? ''),
            'email' => SetupInput::trim(CatalogAdminRoutes::text($body, 'recipientEmail', 254) ?? ''),
        ];
    }
}
