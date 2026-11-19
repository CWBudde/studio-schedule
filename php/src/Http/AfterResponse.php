<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Closure;
use Throwable;

final class AfterResponse
{
    /** @var array<string,array{run:Closure():void,runLast:bool}> */
    private array $jobs = [];

    /** @param Closure():void $run */
    public function wake(string $name, Closure $run, bool $runLast = false): void
    {
        $this->jobs[$name] = ['run' => $run, 'runLast' => $runLast];
    }

    /** @param Closure():bool $finish */
    public function run(Closure $finish): void
    {
        $jobs = $this->jobs;
        $this->jobs = [];
        if ($jobs === [] || !$finish()) {
            return;
        }

        uasort($jobs, static fn(array $a, array $b): int => $a['runLast'] <=> $b['runLast']);
        foreach ($jobs as $name => $job) {
            try {
                $job['run']();
            } catch (Throwable $error) {
                error_log('studio-schedule after-response job ' . $name . ' failed: ' . $error::class);
            }
        }
    }
}
