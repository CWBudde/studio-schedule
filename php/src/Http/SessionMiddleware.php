<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Http\Server\MiddlewareInterface;
use Psr\Http\Server\RequestHandlerInterface;
use Slim\Routing\RouteContext;
use StudioSchedule\Config;
use StudioSchedule\Service\SessionAuthentication;

final readonly class SessionMiddleware implements MiddlewareInterface
{
    public function __construct(private Config $config, private SessionAuthentication $authentication) {}

    public function process(ServerRequestInterface $request, RequestHandlerInterface $handler): ResponseInterface
    {
        $pattern = RouteContext::fromRequest($request)->getRoute()?->getPattern() ?? '';
        if (!str_starts_with($pattern, '/api/v1/')) {
            return $handler->handle($request);
        }

        $cookieName = $this->config->cookieName();
        $headers = [];
        $cookies = new SessionCookies($this->config);
        $supplied = $request->getCookieParams();
        if ($cookieName === '__Host-studio_session' && array_key_exists('studio_session', $supplied)) {
            $headers[] = $cookies->expire('studio_session');
        }

        $token = $supplied[$cookieName] ?? null;
        if (is_string($token) && $token !== '') {
            $request = $request->withAttribute('sessionToken', $token);
            $session = $this->authentication->authenticate($token);
            if ($session === null) {
                $headers[] = $cookies->expire($cookieName);
            } else {
                $request = $request->withAttribute('sessionPrincipal', $session['principal']);
                if ($session['renewedUntil'] !== null) {
                    $headers[] = $cookies->issue($token, $session['renewedUntil']);
                }
            }
        }

        $response = $handler->handle($request);
        // Clear stale cookies before the handler's fresh login cookie.
        return $headers === []
            ? $response
            : $response->withHeader('Set-Cookie', array_merge($headers, $response->getHeader('Set-Cookie')));
    }
}
