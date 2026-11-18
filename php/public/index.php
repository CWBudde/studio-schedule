<?php

declare(strict_types=1);

use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\ResponseEmitter;
use StudioSchedule\Bootstrap;

// This file runs before Composer's autoloader, including on an unsupported host.
if (version_compare(PHP_VERSION, '8.3.0', '<') || PHP_INT_SIZE < 8) {
    http_response_code(503);
    header('Content-Type: text/plain; charset=utf-8');
    header('Cache-Control: no-store');
    exit('Studio Schedule requires 64-bit PHP 8.3 or later.');
}

$bundle = dirname(__DIR__) . '/app';
$packaged = is_file($bundle . '/vendor/autoload.php');
$appDirectory = $packaged ? $bundle : dirname(__DIR__);
require $appDirectory . '/vendor/autoload.php';

$publicDirectory = $packaged ? __DIR__ : dirname($appDirectory) . '/web/dist';
$migrations = $packaged ? $appDirectory . '/migrations/postgres' : dirname($appDirectory) . '/internal/store/postgres/migrations';
$response = Bootstrap::handle(static function () {
    return ServerRequestFactory::createFromGlobals();
}, $appDirectory, $publicDirectory, $migrations);

(new ResponseEmitter())->emit($response);
