<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Id;
use StudioSchedule\Receipt\Attachments;
use StudioSchedule\Service\SessionPrincipal;

final readonly class ReceiptPdfRoutes
{
    public function __construct(private Attachments $receipts) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->get('/api/v1/admin/receipts/{receiptId}/pdf', $this->office(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->get('/api/v1/my/receipts/{receiptId}/pdf', $this->member(...));
    }

    /** @param array{receiptId: string} $args */
    private function office(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        return $this->download($request, $response, $args['receiptId'], null);
    }

    /** @param array{receiptId: string} $args */
    private function member(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        if ($request->getMethod() === 'HEAD') {
            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', 'GET');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }

        return $this->download($request, $response, $args['receiptId'], $principal);
    }

    private function download(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        string $id,
        ?SessionPrincipal $principal,
    ): ResponseInterface {
        if (!Id::valid($id)) {
            return Responses::problem($request, 422, 'validation failed', errors: [['location' => 'path.receiptId', 'message' => 'invalid ID']]);
        }
        $file = $principal === null ? $this->receipts->find($id) : $this->receipts->findForPerson($id, $principal->personId);
        if ($file === null) {
            return Responses::problem($request, 404, 'resource not found');
        }

        $response->getBody()->write($file['pdf']);

        return $response
            ->withHeader('Content-Type', 'application/pdf')
            ->withHeader('Content-Disposition', 'attachment; filename="' . $file['filename'] . '"')
            ->withHeader('Cache-Control', 'private, no-store');
    }
}
