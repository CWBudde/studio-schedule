<?php

declare(strict_types=1);

use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\ResponseEmitter;
use StudioSchedule\Bootstrap;
use StudioSchedule\Http\AfterResponse;

// This file runs before Composer's autoloader, including on an unsupported host.
if (version_compare(PHP_VERSION, '8.3.0', '<') || PHP_INT_SIZE < 8) {
    http_response_code(503);
    header('Content-Type: text/plain; charset=utf-8');
    header('Cache-Control: no-store');
    exit('Studio Schedule requires 64-bit PHP 8.3 or later.');
}

$privateDirectory = (string) getenv('STUDIO_SCHEDULE_APP_DIR');
$bundle = $privateDirectory === '' ? dirname(__DIR__) . '/app' : $privateDirectory;
$packaged = is_file($bundle . '/vendor/autoload.php');
if ($privateDirectory !== '' && !$packaged) {
    http_response_code(503);
    header('Content-Type: text/plain; charset=utf-8');
    header('Cache-Control: no-store');
    exit('The configured Studio Schedule app directory is unavailable.');
}
$appDirectory = $packaged ? $bundle : dirname(__DIR__);
require $appDirectory . '/vendor/autoload.php';

$publicDirectory = $packaged ? __DIR__ : dirname($appDirectory) . '/web/dist';
$migrations = $packaged ? $appDirectory . '/migrations/postgres' : dirname($appDirectory) . '/internal/store/postgres/migrations';
$afterResponse = function_exists('fastcgi_finish_request') ? new AfterResponse() : null;
$response = Bootstrap::handle(static function () {
    return ServerRequestFactory::createFromGlobals();
}, $appDirectory, $publicDirectory, $migrations, afterResponse: $afterResponse);

(new ResponseEmitter())->emit($response);
$afterResponse?->run(fastcgi_finish_request(...));
