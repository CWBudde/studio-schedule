<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Slim\Factory\AppFactory;
use Slim\Psr7\Factory\ServerRequestFactory;
use StudioSchedule\Http\ScheduleRoutes;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\ScheduleStore;

final class ScheduleRoutesTest extends TestCase
{
    public function testRejectsMalformedDatesAndIdentifiersBeforeReadingTheSchedule(): void
    {
        $app = AppFactory::create();
        $app->setBasePath('/dance/schedule');
        $store = new ScheduleStore(Database::connect('postgresql://fixture:fixture@127.0.0.1:1/unused'));
        ScheduleRoutes::register($app, $store, new class implements ClockInterface {
            public function now(): DateTimeImmutable
            {
                return new DateTimeImmutable('2026-10-25T16:59:59Z');
            }
        });
        $app->addRoutingMiddleware();

        foreach ([
            '/schedule?from=2026-11-05&to=2026-11-05' => 'validation failed: to must be after from',
            '/schedule?from=2026-11-05&to=2026-12-18' => 'validation failed: the range must not exceed 42 days',
            '/schedule?from=2000-02-30&location=00000000-0000-0000-0000-000000000000' => [
                'query.from', 'query.to', 'query.location',
            ],
            '/lessons/not-an-id' => ['path.lessonId'],
        ] as $uri => $expected) {
            $request = (new ServerRequestFactory())->createServerRequest('GET', '/dance/schedule/api/v1' . $uri)
                ->withAttribute('requestId', 'schedule-fixture');
            $response = $app->handle($request);
            self::assertSame(422, $response->getStatusCode());
            self::assertSame('application/problem+json', $response->getHeaderLine('Content-Type'));
            $body = json_decode((string) $response->getBody(), true, flags: JSON_THROW_ON_ERROR);
            self::assertIsArray($body);
            self::assertSame('schedule-fixture', $body['requestId']);
            if (is_array($expected)) {
                self::assertIsArray($body['errors']);
                self::assertSame($expected, array_column($body['errors'], 'location'));
                foreach ($body['errors'] as $detail) {
                    self::assertIsArray($detail);
                    self::assertArrayNotHasKey('value', $detail);
                }
            } else {
                self::assertSame($expected, $body['detail']);
            }
        }
    }
}
