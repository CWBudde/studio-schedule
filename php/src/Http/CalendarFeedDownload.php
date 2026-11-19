<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use StudioSchedule\Service\CalendarFeedRendering;
use StudioSchedule\Service\ICalendarWriter;
use StudioSchedule\Service\SettingsNotFound;
use StudioSchedule\Store\RateLimitStore;

final readonly class CalendarFeedDownload
{
    public function __construct(
        private CalendarFeedRendering $feeds,
        private RateLimitStore $limits,
        private TrustedProxies $proxies,
    ) {}

    /** @param array{token:string} $args */
    public function __invoke(
        ServerRequestInterface $request,
        ResponseInterface $response,
        #[SensitiveParameter]
        array $args,
    ): ResponseInterface {
        $download = self::download($request->getUri()->getQuery());
        if ($download === null) {
            return Responses::problem($request, 422, 'validation failed');
        }

        $peer = $request->getServerParams()['REMOTE_ADDR'] ?? '';
        $address = $this->proxies->clientIP(is_string($peer) ? $peer : '', $request->getHeaderLine('X-Forwarded-For'));
        $retry = $this->limits->allow('calendar-ip', $address, 2000, 3600);
        if ($retry === 0) {
            $retry = $this->limits->allow('calendar-token', hash('sha256', $args['token']), 30, 3600);
        }
        if ($retry > 0) {
            return Responses::tooManyAttempts($request, $retry);
        }

        try {
            $feed = $this->feeds->render($args['token']);
        } catch (SettingsNotFound) {
            $feed = null;
        }

        if ($feed === null) {
            return Responses::problem($request, 404, 'resource not found');
        }

        $doc = $feed['document'];
        $etag = '"' . $doc->fingerprint . '"';
        $response = $response->withHeader('Cache-Control', 'private, max-age=900')->withHeader('ETag', $etag);
        if (Responses::etagMatches($request->getHeaderLine('If-None-Match'), $etag)) {
            return $response->withStatus(304);
        }

        $response->getBody()->write($doc->body);

        return $response
            ->withHeader('Content-Type', ICalendarWriter::CONTENT_TYPE)
            ->withHeader('Content-Disposition', $download ? 'attachment; filename="' . $feed['filename'] . '"' : 'inline');
    }

    private static function download(string $query): ?bool
    {
        foreach (explode('&', $query) as $part) {
            $pair = explode('=', $part, 2);
            if (urldecode($pair[0]) === 'download') {
                return match (urldecode($pair[1] ?? 'true')) {
                    '1', 't', 'T', 'true', 'TRUE', 'True' => true,
                    '', '0', 'f', 'F', 'false', 'FALSE', 'False' => false,
                    default => null,
                };
            }
        }

        return false;
    }
}
