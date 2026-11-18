<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateInterval;
use Psr\Clock\ClockInterface;
use Psr\Log\LoggerInterface;
use StudioSchedule\Store\JobLocks;
use StudioSchedule\Store\JobRunStore;
use Throwable;

final readonly class JobRunner
{
    public function __construct(
        private JobLocks $locks,
        private JobRunStore $runs,
        private ClockInterface $clock,
        private LoggerInterface $logger,
    ) {}

    /** @param callable():void $job */
    public function runDue(string $name, DateInterval $every, callable $job): ?JobResult
    {
        if ($this->runs->ranSince($name, $this->clock->now()->sub($every))) {
            return null;
        }

        $result = null;
        $acquired = $this->locks->runIfFree($name, function () use ($name, $every, $job, &$result): void {
            $start = $this->clock->now();
            if ($this->runs->ranSince($name, $start->sub($every))) {
                return; // a competing scheduler may just have completed it
            }

            $result = JobResult::Ok;
            try {
                $job();
            } catch (Throwable $error) {
                $this->logger->error('background job failed', ['job' => $name, 'exception' => $error::class]);
                $result = JobResult::Failed;
            }
            // Publish the run before releasing the job lock so the next
            // scheduler sees it when it checks the interval.
            $this->runs->record($name, $start, $this->clock->now(), $result);
        });
        if (!$acquired) {
            $now = $this->clock->now();
            $result = JobResult::Skipped;
            $this->runs->record($name, $now, $now, $result);
        }

        return $result;
    }
}
