<?php

declare(strict_types=1);

use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\ResponseEmitter;
use StudioSchedule\Tests\Conformance\FixtureServer;

$app = dirname(__DIR__, 2);
require $app . '/vendor/autoload.php';

try {
    $fixture = FixtureServer::fromEnvironment($app, dirname($app));
    if (PHP_SAPI === 'cli' && ($argv[1] ?? '') === '--check') {
        exit(0);
    }
    if (PHP_SAPI !== 'cli-server') {
        throw new RuntimeException('The conformance router requires the local PHP CLI server.');
    }

    (new ResponseEmitter())->emit($fixture->handle(ServerRequestFactory::createFromGlobals()));
} catch (Throwable $error) {
    if (PHP_SAPI === 'cli') {
        fwrite(STDERR, "PHP conformance fixture configuration refused.\n");
        exit(1);
    }
    error_log('PHP conformance fixture refused: ' . $error::class);
    http_response_code(503);
    header('Cache-Control: no-store');
}
