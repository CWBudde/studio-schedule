<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SetupInput;
use StudioSchedule\Store\ReceiptReadStore;

final readonly class ReceiptReadRoutes
{
    public function __construct(private ReceiptReadStore $receipts) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->post('/api/v1/admin/receipts/search', $this->search(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->get('/api/v1/admin/receipts/{receiptId}', $this->detail(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->get('/api/v1/my/receipts', $this->mine(...));
    }

    private function search(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $body = JsonObject::read($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $fields = array_change_key_case(get_object_vars($body));
        if (array_diff(array_keys($fields), ['year', 'query', 'offset', 'limit']) !== []) {
            return self::invalid($request, 'body', 'unexpected property');
        }

        $year = $fields['year'] ?? 0;
        $query = $fields['query'] ?? '';
        $offset = $fields['offset'] ?? 0;
        $limit = $fields['limit'] ?? 50;
        if (!is_int($year) || $year < 0 || $year > 9999) {
            return self::invalid($request, 'body.year', 'expected a year between 0 and 9999');
        }
        if (!is_string($query) || mb_strlen($query, 'UTF-8') > 100 || str_contains($query, "\0")) {
            return self::invalid($request, 'body.query', 'expected a query of at most 100 characters');
        }
        if (!is_int($offset) || $offset < 0) {
            return self::invalid($request, 'body.offset', 'expected a non-negative integer');
        }
        if (!is_int($limit) || $limit < 1 || $limit > 100) {
            return self::invalid($request, 'body.limit', 'expected a limit between 1 and 100');
        }

        return Responses::json($response, $this->receipts->search($year, SetupInput::trim($query), $offset, $limit));
    }

    /** @param array{receiptId:string} $args */
    private function detail(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        if (!Id::valid($args['receiptId'])) {
            return self::invalid($request, 'path.receiptId', 'invalid ID');
        }
        $receipt = $this->receipts->detail($args['receiptId']);

        return $receipt === null ? Responses::problem($request, 404, 'resource not found') : Responses::json($response, $receipt);
    }

    private function mine(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        if ($request->getMethod() === 'HEAD') {
            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', 'GET');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }

        return Responses::json($response, ['items' => $this->receipts->mine($principal->personId)]);
    }

    private static function invalid(#[SensitiveParameter] ServerRequestInterface $request, string $location, string $message): ResponseInterface
    {
        return Responses::problem($request, 422, 'validation failed', errors: [['location' => $location, 'message' => $message]]);
    }
}
