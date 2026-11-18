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
    private const string PATH = '/api/v1/settings/';

    public function __construct(private PublicSettings $settings) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->get(self::PATH . 'public', $this->publicSettings(...));
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
