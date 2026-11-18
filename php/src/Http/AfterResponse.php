<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Closure;
use Throwable;

final class AfterResponse
{
    /** @var array<string,Closure():void> */
    private array $jobs = [];

    /** @param Closure():void $run */
    public function wake(string $name, Closure $run): void
    {
        $this->jobs[$name] = $run;
    }

    /** @param Closure():bool $finish */
    public function run(Closure $finish): void
    {
        $jobs = $this->jobs;
        $this->jobs = [];
        if ($jobs === [] || !$finish()) {
            return;
        }

        foreach ($jobs as $name => $job) {
            try {
                $job();
            } catch (Throwable $error) {
                error_log('studio-schedule after-response job ' . $name . ' failed: ' . $error::class);
            }
        }
    }
}
