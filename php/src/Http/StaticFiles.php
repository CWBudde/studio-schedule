<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Slim\Psr7\Factory\StreamFactory;
use Slim\Psr7\Response;

final readonly class StaticFiles
{
    private const TYPES = [
        'css' => 'text/css; charset=utf-8',
        'html' => 'text/html; charset=utf-8',
        'ico' => 'image/x-icon',
        'js' => 'text/javascript; charset=utf-8',
        'jpg' => 'image/jpeg',
        'jpeg' => 'image/jpeg',
        'png' => 'image/png',
        'svg' => 'image/svg+xml',
        'webp' => 'image/webp',
        'woff2' => 'font/woff2',
        'webmanifest' => 'application/manifest+json',
    ];

    public function __construct(private string $directory, private string $basePath) {}

    public function serve(string $path, ServerRequestInterface $request): ?ResponseInterface
    {
        if ($path === '/api' || str_starts_with($path, '/api/') || str_starts_with($path, '/ical/')
            || $path === MetricsEndpoint::PATH || $path === '/setup' || str_starts_with($path, '/setup/')
            || str_contains($path, "\0") || preg_match('#/(?:\.|\.\.)(?:/|$)#', $path) === 1) {
            return null;
        }

        $method = $request->getMethod();
        if ($method !== 'GET' && $method !== 'HEAD') {
            return Responses::problem($request, 405, 'method not allowed')->withHeader('Allow', 'GET, HEAD');
        }

        if ($path === '/' || $path === '/index.html') {
            return $this->index($method === 'HEAD');
        }
        $extension = pathinfo($path, PATHINFO_EXTENSION);
        if (isset(self::TYPES[$extension])) {
            return $this->file($path, self::TYPES[$extension], $method === 'HEAD');
        }
        if ($extension !== '' || str_starts_with($path, '/assets/')) {
            return null;
        }

        return $this->index($method === 'HEAD');
    }

    private function index(bool $head): ?ResponseInterface
    {
        $file = $this->resolve('/index.html');
        if ($file === null) {
            return null;
        }

        $response = (new Response(200))->withHeader('Content-Type', self::TYPES['html'])
            ->withHeader('Cache-Control', 'no-cache');
        if (!$head) {
            $body = file_get_contents($file);
            if ($body === false) {
                return null;
            }
            $base = htmlspecialchars($this->basePath . '/', ENT_QUOTES, 'UTF-8');
            $response->getBody()->write(str_replace('<base href="./">', '<base href="' . $base . '">', $body));
        }

        return $response;
    }

    private function file(string $path, string $mime, bool $head): ?ResponseInterface
    {
        $file = $this->resolve($path);
        if ($file === null) {
            return null;
        }

        $response = (new Response(200))->withHeader('Content-Type', $mime);
        $response = match (true) {
            str_starts_with($path, '/assets/') => $response->withHeader('Cache-Control', 'public, max-age=31536000, immutable'),
            $path === '/sw.js' => $response->withHeader('Cache-Control', 'no-cache'),
            default => $response,
        };

        return $head ? $response : $response->withBody((new StreamFactory())->createStreamFromFile($file));
    }

    private function resolve(string $path): ?string
    {
        $root = realpath($this->directory);
        if ($root === false) {
            return null;
        }

        $file = realpath($root . $path);

        return $file !== false && str_starts_with($file, $root . DIRECTORY_SEPARATOR) && is_file($file) ? $file : null;
    }
}
