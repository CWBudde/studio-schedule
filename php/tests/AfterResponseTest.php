<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use PHPUnit\Framework\TestCase;
use StudioSchedule\Http\AfterResponse;

final class AfterResponseTest extends TestCase
{
    public function testAJobRunsOnceAfterTheResponseIsFinished(): void
    {
        $events = [];
        $after = new AfterResponse();
        $finish = static function () use (&$events): bool {
            $events[] = 'response flushed';

            return true;
        };
        $deliver = static function () use (&$events): void {
            $events[] = 'mail delivered';
        };

        $after->run($finish);
        $after->wake('deliver-mail', $deliver);
        $after->wake('deliver-mail', $deliver);
        self::assertSame([], $events);
        $after->run($finish);
        $after->run($finish);
        self::assertSame(['response flushed', 'mail delivered'], $events);
    }

    public function testTheJobWaitsForCronIfTheResponseCannotBeFinished(): void
    {
        $after = new AfterResponse();
        $runs = 0;
        $after->wake('deliver-mail', static function () use (&$runs): void {
            ++$runs;
        });
        $after->run(static fn(): bool => false);
        $after->run(static fn(): bool => true);
        self::assertSame(0, $runs);
    }
}
