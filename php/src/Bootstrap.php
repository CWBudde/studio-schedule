<?php

declare(strict_types=1);

namespace StudioSchedule;

use Monolog\Formatter\JsonFormatter;
use Monolog\Handler\StreamHandler;
use Monolog\Level;
use Monolog\Logger;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Slim\Psr7\Factory\ServerRequestFactory;
use StudioSchedule\Http\Application;
use StudioSchedule\Http\Responses;
use StudioSchedule\Service\DatabaseReadiness;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Schema;
use Symfony\Component\Uid\Uuid;
use Throwable;

final class Bootstrap
{
    /**
     * @param callable():ServerRequestInterface $makeRequest
     * @param (callable(string): ?string)|null $environment
     */
    public static function handle(
        callable $makeRequest,
        string $appDirectory,
        string $publicDirectory,
        string $postgresMigrations,
        ?callable $environment = null,
    ): ResponseInterface {
        $request = null;
        try {
            $request = $makeRequest();
            $config = Config::load($appDirectory . '/config/.env', $environment);
            $logger = new Logger('studio-schedule');
            $level = match ($config->logLevel) {
                'debug' => Level::Debug,
                'warn' => Level::Warning,
                'error' => Level::Error,
                default => Level::Info,
            };
            $handler = new StreamHandler('php://stderr', $level);
            if ($config->logFormat === 'json') {
                $handler->setFormatter(new JsonFormatter());
            }
            $logger->pushHandler($handler);

            $connection = $config->databaseURL === '' ? null : Database::connect($config->databaseURL);
            $schema = $connection === null ? null : new Schema($connection);
            $scheme = parse_url($config->databaseURL, PHP_URL_SCHEME);
            $postgres = is_string($scheme) && in_array(strtolower($scheme), ['postgresql', 'postgres'], true);
            $migrations = $postgres ? $postgresMigrations : $appDirectory . '/migrations/mysql';
            $readiness = new DatabaseReadiness($schema, Schema::migrationVersions($migrations), $logger);

            return Application::create($config, $publicDirectory, $readiness, $logger)->handle($request);
        } catch (Throwable $error) {
            $id = Uuid::v7()->toRfc4122();
            error_log('studio-schedule startup error ' . $id . ': ' . $error::class);
            $request ??= (new ServerRequestFactory())->createServerRequest('GET', '/');
            $response = Responses::problem($request->withAttribute('requestId', $id), 500, 'internal server error');

            return Responses::secure($response, $id);
        }
    }
}
