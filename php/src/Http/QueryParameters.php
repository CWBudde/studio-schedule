<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ServerRequestInterface;

final class QueryParameters
{
    /** @return array<string,string> */
    public static function read(ServerRequestInterface $request): array
    {
        $params = [];
        // The Go router reads the first value of each name. parse_str() also
        // interprets bracketed names as arrays and renames keys with spaces.
        foreach (explode('&', $request->getUri()->getQuery()) as $encoded) {
            $pair = explode('=', $encoded, 2);
            $name = urldecode($pair[0]);
            $params[$name] ??= isset($pair[1]) ? urldecode($pair[1]) : 'true';
        }

        return $params;
    }
}
