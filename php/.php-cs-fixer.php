<?php

declare(strict_types=1);

use PhpCsFixer\Config;
use PhpCsFixer\Finder;

return (new Config())
    ->setRules(['@PER-CS' => true])
    ->setUsingCache(false)
    ->setFinder((new Finder())->in([__DIR__ . '/src', __DIR__ . '/public', __DIR__ . '/tests', __DIR__ . '/tools']));
