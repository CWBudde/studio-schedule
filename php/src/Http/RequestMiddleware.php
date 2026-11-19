<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Closure;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Http\Server\MiddlewareInterface;
use Psr\Http\Server\RequestHandlerInterface;
use Psr\Log\LoggerInterface;
use Slim\Exception\HttpMethodNotAllowedException;
use Slim\Exception\HttpNotFoundException;
use Slim\Psr7\Response;
use StudioSchedule\Config;
use StudioSchedule\Service\Readiness;
use Symfony\Component\Uid\Uuid;
use Throwable;

final readonly class RequestMiddleware implements MiddlewareInterface
{
    /** @param (Closure(string,string,int,int):void)|null $observeRequest */
    public function __construct(
        private Config $config,
        private StaticFiles $staticFiles,
        private Readiness $readiness,
        private LoggerInterface $logger,
        private ?Closure $observeRequest = null,
    ) {}

    public function process(ServerRequestInterface $request, RequestHandlerInterface $handler): ResponseInterface
    {
        $id = $this->requestId($request);
        $request = $request->withAttribute('requestId', $id);
        $path = rawurldecode($request->getUri()->getPath());
        $localPath = $this->localPath($path);
        $redacted = self::redact($localPath ?? $path);
        $start = hrtime(true);
        $observation = null;
        if ($this->observeRequest !== null && !in_array($localPath, [MetricsEndpoint::PATH, '/healthz', '/readyz'], true)) {
            $observation = new RequestMetricRoute();
            $request = $request->withAttribute(RequestMetricRoute::class, $observation);
        }

        try {
            $response = $this->dispatch($request, $handler, $localPath, $redacted);
        } catch (Throwable $error) {
            $this->logger->error('request failed', ['requestId' => $id, 'path' => $redacted, 'exception' => $error::class]);
            $response = Responses::problem($request, 500, 'internal server error');
        }

        $duration = hrtime(true) - $start;
        if ($observation !== null) {
            ($this->observeRequest)($request->getMethod(), $observation->pattern, $response->getStatusCode(), intdiv($duration, 1_000));
        }

        $this->logger->log($response->getStatusCode() >= 500 ? 'error' : 'info', 'http request', [
            'requestId' => $id,
            'method' => $request->getMethod(),
            'path' => $redacted,
            'status' => $response->getStatusCode(),
            'durationMs' => $duration / 1_000_000,
        ]);

        $response = Responses::secure($response, $id);
        if ($localPath === '/setup') {
            $response = $response->withHeader('Referrer-Policy', 'no-referrer');
        }
        if ($this->config->secure) {
            $hsts = 'max-age=31536000' . ($this->config->hstsSubdomains ? '; includeSubDomains' : '');
            $response = $response->withHeader('Strict-Transport-Security', $hsts);
        }
        if (self::apiPath($localPath ?? '') && $response->getHeaderLine('Content-Type') !== 'text/html') {
            if (!$response->hasHeader('Cache-Control')) {
                $response = $response->withHeader('Cache-Control', 'no-store');
            }
            if (!$response->hasHeader('Content-Disposition')) {
                $response = $response->withHeader('Content-Disposition', 'attachment; filename="api.json"');
            }
        }

        return $response;
    }

    private function requestId(ServerRequestInterface $request): string
    {
        $peer = $request->getServerParams()['REMOTE_ADDR'] ?? null;
        $header = $request->getHeaderLine('X-Request-Id');
        if (is_string($peer) && preg_match('#^[A-Za-z0-9._~:+/=@-]{1,128}$#D', $header) === 1
            && $this->config->trustedProxies->contains($peer)) {
            return $header;
        }

        return Uuid::v7()->toRfc4122();
    }

    private function dispatch(
        ServerRequestInterface $request,
        RequestHandlerInterface $handler,
        ?string $path,
        string $redacted,
    ): ResponseInterface {
        if ($path === null) {
            return Responses::problem($request, 404, 'no route outside the app base path');
        }
        if ($path === '' && $this->config->basePath !== '') {
            $query = $request->getUri()->getQuery();
            $location = $this->config->basePath . '/' . ($query === '' ? '' : '?' . $query);

            return (new Response(308))->withHeader('Location', $location);
        }
        if (self::crossOrigin($request)) {
            return Responses::problem($request, 403, 'cross-origin request rejected');
        }
        if ((self::apiPath($path) || $path === PublicSettingsEndpoint::MANIFEST_PATH || str_starts_with($path, '/ical/')) && !$this->readiness->ready()) {
            return Responses::problem($request, 503, 'maintenance', 'urn:studio-schedule:maintenance');
        }

        try {
            return $handler->handle($request);
        } catch (HttpMethodNotAllowedException $error) {
            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', implode(', ', $error->getAllowedMethods()));
        } catch (HttpNotFoundException) {
            return $this->staticFiles->serve($path, $request)
                ?? Responses::problem($request, 404, 'no route for ' . $redacted);
        }
    }

    private function localPath(string $path): ?string
    {
        $prefix = $this->config->basePath;
        if ($prefix === '') {
            return $path === '' ? '/' : $path;
        }

        return $path === $prefix || str_starts_with($path, $prefix . '/') ? substr($path, strlen($prefix)) : null;
    }

    private static function apiPath(string $path): bool
    {
        return $path === '/api' || str_starts_with($path, '/api/');
    }

    private static function crossOrigin(ServerRequestInterface $request): bool
    {
        if (in_array($request->getMethod(), ['GET', 'HEAD', 'OPTIONS'], true)) {
            return false;
        }

        $site = $request->getHeaderLine('Sec-Fetch-Site');
        if ($site !== '') {
            return !in_array($site, ['same-origin', 'none'], true);
        }

        $value = $request->getHeaderLine('Origin');
        if ($value === '') {
            return false;
        }

        $origin = parse_url($value);
        if ($origin === false || !isset($origin['host'])) {
            return true;
        }
        $authority = $origin['host'] . (isset($origin['port']) ? ':' . $origin['port'] : '');

        return $authority !== $request->getHeaderLine('Host');
    }

    private static function redact(string $path): string
    {
        $feed = preg_replace_callback(
            '#(/ical/).+#s',
            static fn(array $match): string => $match[1] . '<redacted>' . (str_ends_with($match[0], '.ics') ? '.ics' : ''),
            $path,
        ) ?? $path;

        return preg_replace(
            '#(/api/v1/(?:event-invites|waitlist-invites)/).+#s',
            '$1<redacted>',
            $feed,
        ) ?? $feed;
    }
}
