<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateInterval;
use Psr\Clock\ClockInterface;
use Psr\Log\LoggerInterface;
use StudioSchedule\Store\JobLocks;
use StudioSchedule\Store\JobRunStore;
use StudioSchedule\Store\MetricsStore;
use Throwable;

final readonly class JobRunner
{
    public function __construct(
        private JobLocks $locks,
        private JobRunStore $runs,
        private ClockInterface $clock,
        private LoggerInterface $logger,
        private ?MetricsStore $metrics = null,
    ) {}

    /** @param callable():void $job */
    public function runDue(string $name, DateInterval $every, callable $job): ?JobResult
    {
        if ($this->runs->ranSince($name, $this->clock->now()->sub($every))) {
            return null;
        }

        return $this->run($name, $job, $every);
    }

    /** @param callable():void $job */
    public function runNow(string $name, callable $job): ?JobResult
    {
        return $this->run($name, $job, null);
    }

    /** @param callable():void $job */
    private function run(string $name, callable $job, ?DateInterval $every): ?JobResult
    {
        $result = null;
        $observation = null;
        $acquired = $this->locks->runIfFree($name, function () use ($name, $every, $job, &$result, &$observation): void {
            $start = $this->clock->now();
            if ($every !== null && $this->runs->ranSince($name, $start->sub($every))) {
                return; // a competing scheduler may just have completed it
            }

            $result = JobResult::Ok;
            $started = hrtime(true);
            try {
                $job();
            } catch (Throwable $error) {
                $this->logger->error('background job failed', ['job' => $name, 'exception' => $error::class]);
                $result = JobResult::Failed;
            }
            $duration = intdiv(hrtime(true) - $started, 1_000);
            $end = $this->clock->now();
            // Publish the run before releasing the job lock so the next
            // scheduler sees it when it checks the interval.
            $this->runs->record($name, $start, $end, $result);
            $observation = [$result, $duration, $end];
        });
        if (!$acquired) {
            $now = $this->clock->now();
            $result = JobResult::Skipped;
            $this->runs->record($name, $now, $now, $result);
        }

        if ($observation !== null) {
            [$observedResult, $duration, $end] = $observation;
            $this->metrics?->observeJob($name, $observedResult, $duration, $end);
        }

        return $result;
    }
}
