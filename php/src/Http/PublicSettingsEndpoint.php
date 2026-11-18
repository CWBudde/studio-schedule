<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Slim\App;
use StudioSchedule\Domain\BrandingImage;
use StudioSchedule\Service\PublicSettings;
use StudioSchedule\Service\SettingsNotFound;

final readonly class PublicSettingsEndpoint
{
    public const string MANIFEST_PATH = '/manifest.webmanifest';

    private const string PATH = '/api/v1/settings/';

    public function __construct(private PublicSettings $settings) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->get(self::PATH . 'public', $this->publicSettings(...));
        $base = $app->getBasePath();
        $app->get(self::MANIFEST_PATH, fn(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
            => $this->manifest($request, $response, $base));
        foreach (BrandingImage::cases() as $image) {
            $app->get(self::PATH . $image->value, fn(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
                => $this->image($request, $response, $image));
        }
    }

    private function publicSettings(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        try {
            return Responses::json($response, $this->settings->settings());
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        }
    }

    private function manifest(ServerRequestInterface $request, ResponseInterface $response, string $base): ResponseInterface
    {
        try {
            $settings = $this->settings->settings();
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        }

        $root = $base . '/';
        $name = $settings['schoolName'];
        $lang = $settings['defaultLocale'];
        $icons = [
            ['src' => $base . '/icons/icon-192.png', 'sizes' => '192x192', 'type' => 'image/png'],
            ['src' => $base . '/icons/icon-512.png', 'sizes' => '512x512', 'type' => 'image/png'],
            ['src' => $base . '/icons/icon-maskable-512.png', 'sizes' => '512x512', 'type' => 'image/png', 'purpose' => 'maskable'],
        ];

        return Responses::json($response, [
            'id' => $root,
            'name' => $name,
            'short_name' => $name,
            'description' => $lang === 'de' ? 'Stundenplan und Buchung' : 'Class schedule and booking',
            'lang' => $lang,
            'start_url' => $root,
            'scope' => $root,
            'display' => 'standalone',
            'theme_color' => $settings['accentColor'] ?? '#7f1f35',
            'background_color' => '#faf7f2',
            'icons' => $icons,
        ])->withHeader('Content-Type', 'application/manifest+json')->withHeader('Cache-Control', 'public, max-age=300');
    }

    private function image(ServerRequestInterface $request, ResponseInterface $response, BrandingImage $image): ResponseInterface
    {
        $ifNoneMatch = $request->getHeaderLine('If-None-Match');
        try {
            $asset = $this->settings->image($image, static fn(string $digest): bool => self::matches($ifNoneMatch, self::etag($digest)));
        } catch (SettingsNotFound) {
            return Responses::problem($request, 404, 'resource not found');
        }
        if ($asset === null) {
            return Responses::problem($request, 404, 'no ' . $image->value);
        }

        $response = $response
            ->withHeader('ETag', self::etag($asset['digest']))
            ->withHeader('Cache-Control', 'public, no-cache')
            ->withHeader('Content-Disposition', 'inline');
        if (!isset($asset['data'])) {
            return $response->withStatus(304);
        }

        $response->getBody()->write($asset['data']);

        return $response->withHeader('Content-Type', $asset['contentType']);
    }

    private static function etag(string $digest): string
    {
        return '"' . $digest . '"';
    }

    private static function matches(string $header, string $etag): bool
    {
        foreach (explode(',', $header) as $candidate) {
            $candidate = trim($candidate);
            if ($candidate === '*' || $candidate === $etag || $candidate === 'W/' . $etag) {
                return true;
            }
        }

        return false;
    }
}
