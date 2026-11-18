<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Http\Server\RequestHandlerInterface;
use Psr\Log\LoggerInterface;
use Slim\Factory\AppFactory;
use Slim\Psr7\Factory\ResponseFactory;
use StudioSchedule\Config;
use StudioSchedule\Service\Readiness;

final class Application
{
    /** @param (callable(ServerRequestInterface, ResponseInterface):ResponseInterface)|null $resetPassword */
    public static function create(
        Config $config,
        string $publicDirectory,
        Readiness $readiness,
        LoggerInterface $logger,
        ?Installer $installer = null,
        ?callable $resetPassword = null,
    ): RequestHandlerInterface {
        $app = AppFactory::create(new ResponseFactory());
        $app->setBasePath($config->basePath);

        $healthy = static fn(ServerRequestInterface $r, ResponseInterface $res): ResponseInterface
            => Responses::json($res, ['status' => 'ok'], $r->getMethod() === 'HEAD')
                ->withHeader('Cache-Control', 'no-store');

        $app->map(['GET', 'HEAD'], '/healthz', $healthy);
        $app->map(['GET', 'HEAD'], '/readyz', static function (
            ServerRequestInterface $r,
            ResponseInterface $res,
        ) use ($readiness, $healthy): ResponseInterface {
            return $readiness->ready() ? $healthy($r, $res) : Responses::problem($r, 503, 'not ready');
        });
        $app->get('/api/v1/health', $healthy);
        if ($resetPassword !== null) {
            $app->post('/api/v1/auth/password-reset/confirm', $resetPassword);
        }
        if ($installer !== null) {
            $app->map(['GET', 'POST'], '/setup', $installer);
        }

        $app->addRoutingMiddleware();
        $app->add(new RequestMiddleware($config, new StaticFiles($publicDirectory, $config->basePath), $readiness, $logger));

        return $app;
    }
}
