<?php

declare(strict_types=1);

use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\ResponseEmitter;
use StudioSchedule\Bootstrap;

$bundle = dirname(__DIR__) . '/app';
$packaged = is_file($bundle . '/vendor/autoload.php');
$appDirectory = $packaged ? $bundle : dirname(__DIR__);
require $appDirectory . '/vendor/autoload.php';

$publicDirectory = $packaged ? __DIR__ : dirname($appDirectory) . '/web/dist';
$migrations = $packaged ? $appDirectory . '/migrations/postgres' : dirname($appDirectory) . '/internal/store/postgres/migrations';
$response = Bootstrap::handle(ServerRequestFactory::createFromGlobals(...), $appDirectory, $publicDirectory, $migrations);

(new ResponseEmitter())->emit($response);
