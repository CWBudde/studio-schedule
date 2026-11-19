<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Doctrine\DBAL\Exception;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Log\LoggerInterface;
use SensitiveParameter;
use StudioSchedule\Service\PrometheusMetrics;
use StudioSchedule\Store\MetricsStore;

final readonly class MetricsEndpoint
{
    public const string PATH = '/metrics';

    public function __construct(
        #[SensitiveParameter]
        private string $token,
        private ?MetricsStore $metrics,
        private string $version,
        private LoggerInterface $logger,
    ) {}

    public function __invoke(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $authorization = $request->getHeaderLine('Authorization');
        if (strncasecmp($authorization, 'Bearer ', 7) !== 0 || !hash_equals($this->token, substr($authorization, 7))) {
            return Responses::problem($request, 401, 'metrics bearer token required')->withHeader('WWW-Authenticate', 'Bearer');
        }

        if ($this->metrics === null) {
            return Responses::problem($request, 503, 'metrics unavailable');
        }

        try {
            $head = $request->getMethod() === 'HEAD';
            $snapshot = $this->metrics->snapshot(includeOutbox: !$head);
            if (!$head) {
                $response->getBody()->write(PrometheusMetrics::render($snapshot['samples'], $snapshot['outbox'], $this->version));
            }
        } catch (Exception $error) {
            $this->logger->warning('metrics: scrape failed', ['exception' => $error::class]);

            return Responses::problem($request, 503, 'metrics unavailable');
        }

        return $response->withHeader('Content-Type', 'text/plain; version=0.0.4; charset=utf-8')
            ->withHeader('Cache-Control', 'no-store');
    }
}
