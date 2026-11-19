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
use StudioSchedule\Service\LessonNoticeLimit;
use StudioSchedule\Service\LessonNotices;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;

final readonly class LessonNoticeRoutes
{
    public function __construct(private LessonNotices $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->post('/api/v1/lessons/{lessonId}/notify', $this->notify(...));
    }

    /** @param array{lessonId:string} $args */
    private function notify(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        if (!in_array($principal->role, ['instructor', 'office', 'admin'], true)) {
            return Responses::problem($request, 403, 'forbidden');
        }

        try {
            $body = CatalogAdminRoutes::fields($request, ['message']);
            if ($body instanceof ResponseInterface) {
                return $body;
            }
            $id = $args['lessonId'];
            if (!Id::valid($id) || stripos($request->getUri()->getPath(), '%2f') !== false) {
                throw new CatalogFailure(422, 'invalid ID', 'path.lessonId');
            }
            $message = CatalogAdminRoutes::text($body, 'message', 4000, true, allowNul: true);
            $count = $this->service->send(strtolower($id), $principal, $message);

            return Responses::json($response, ['recipients' => $count])->withHeader('Cache-Control', 'no-store');
        } catch (CatalogFailure $failure) {
            return CatalogAdminRoutes::failure($request, $failure);
        } catch (LessonNoticeLimit $limit) {
            return Responses::problem($request, 429, $limit->getMessage())->withHeader('Retry-After', (string) $limit->retryAfter);
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        }
    }
}
