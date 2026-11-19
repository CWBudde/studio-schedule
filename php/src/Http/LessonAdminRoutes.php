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
use StudioSchedule\Service\LessonAdministration;
use StudioSchedule\Service\LessonRoomTaken;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SetupInput;

final readonly class LessonAdminRoutes
{
    public function __construct(private LessonAdministration $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->put('/api/v1/lessons/{lessonId}', $this->update(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->post('/api/v1/lessons/{lessonId}/restore', $this->restore(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->post('/api/v1/lessons/{lessonId}/cancel', $this->cancel(...))->add(CatalogAdminRoutes::requireOffice(...));
        $app->delete('/api/v1/lessons/{lessonId}', $this->delete(...))->add(CatalogAdminRoutes::requireOffice(...));
    }

    /** @param array{lessonId:string} $args */
    private function update(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = self::lessonId($request, $args['lessonId']);
        $data = CatalogAdminRoutes::lessonInput($request);
        if ($data instanceof ResponseInterface) {
            return $data;
        }
        try {
            return Responses::json($response, $this->service->update($id, $data))->withHeader('Cache-Control', 'no-store');
        } catch (LessonRoomTaken $error) {
            return CatalogAdminRoutes::roomTaken($request, $error);
        }
    }

    /** @param array{lessonId:string} $args */
    private function restore(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = self::lessonId($request, $args['lessonId']);
        $body = CatalogAdminRoutes::fields($request, ['confirmroomoverlap'], optional: true);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        try {
            $result = $this->service->restore($id, CatalogAdminRoutes::flag($body, 'confirmRoomOverlap'));

            return Responses::json($response, $result)->withHeader('Cache-Control', 'no-store');
        } catch (LessonRoomTaken $error) {
            return CatalogAdminRoutes::roomTaken($request, $error);
        }
    }

    /** @param array{lessonId:string} $args */
    private function cancel(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = self::lessonId($request, $args['lessonId']);
        $body = CatalogAdminRoutes::fields($request, ['note']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $note = $body['note'] ?? '';
        if (!is_string($note) || str_contains($note, "\0")) {
            throw CatalogFailure::field('note', 'expected text');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);

        return Responses::json($response, $this->service->cancel($id, SetupInput::trim($note), $principal))
            ->withHeader('Cache-Control', 'no-store');
    }

    /** @param array{lessonId:string} $args */
    private function delete(ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $this->service->delete(self::lessonId($request, $args['lessonId']));

        return $response->withStatus(204);
    }

    private static function lessonId(ServerRequestInterface $request, string $id): string
    {
        if (!Id::valid($id) || stripos($request->getUri()->getPath(), '%2f') !== false) {
            throw new CatalogFailure(422, 'invalid ID', 'path.lessonId');
        }

        return strtolower($id);
    }
}
