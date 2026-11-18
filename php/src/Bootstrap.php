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
use StudioSchedule\Http\Installer;
use StudioSchedule\Http\PasswordResetEndpoint;
use StudioSchedule\Http\Responses;
use StudioSchedule\Mail\Renderer;
use StudioSchedule\Service\CommonPasswords;
use StudioSchedule\Service\DatabaseReadiness;
use StudioSchedule\Service\PasswordHash;
use StudioSchedule\Service\PasswordReset;
use StudioSchedule\Service\SystemClock;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\PasswordResetStore;
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
        ?string $commonPasswordFile = null,
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

            $setup = rawurldecode($request->getUri()->getPath()) === $config->basePath . '/setup';
            $connection = $setup || $config->databaseURL === '' ? null : Database::connect($config->databaseURL);
            $schema = $connection === null ? null : new Schema($connection);
            $scheme = parse_url($config->databaseURL, PHP_URL_SCHEME);
            $postgres = is_string($scheme) && in_array(strtolower($scheme), ['postgresql', 'postgres'], true);
            $migrations = $postgres ? $postgresMigrations : $appDirectory . '/migrations/mysql';
            $readiness = new DatabaseReadiness($schema, Schema::migrationVersions($migrations), $logger);

            $installer = $config->setupToken === '' ? null : new Installer($config, $migrations, $logger);

            $commonPasswordFile ??= is_file($appDirectory . '/common-passwords.txt')
                ? $appDirectory . '/common-passwords.txt'
                : dirname($appDirectory) . '/internal/auth/password/common-passwords.txt';
            $resetPassword = $connection === null ? null : static function (
                ServerRequestInterface $request,
                ResponseInterface $response,
            ) use ($connection, $commonPasswordFile): ResponseInterface {
                $reset = new PasswordReset(
                    new PasswordResetStore($connection),
                    new PasswordHash(),
                    new CommonPasswords($commonPasswordFile),
                    new SystemClock(),
                    new Renderer(),
                );
                return (new PasswordResetEndpoint($reset))($request, $response);
            };

            return Application::create($config, $publicDirectory, $readiness, $logger, $installer, $resetPassword)->handle($request);
        } catch (Throwable $error) {
            $id = Uuid::v7()->toRfc4122();
            error_log('studio-schedule startup error ' . $id . ': ' . $error::class);
            $request ??= (new ServerRequestFactory())->createServerRequest('GET', '/');
            $response = Responses::problem($request->withAttribute('requestId', $id), 500, 'internal server error');

            return Responses::secure($response, $id);
        }
    }
}
