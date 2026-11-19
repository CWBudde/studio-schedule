<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;

final class RateLimitReservation
{
    private bool $finished = false;

    /** @param Closure(bool, bool): void $finish */
    public function __construct(private readonly Closure $finish) {}

    public function count(): void
    {
        $this->finish(count: true);
    }

    public function release(): void
    {
        $this->finish();
    }

    public function clearFailures(): void
    {
        $this->finish(clear: true);
    }

    private function finish(bool $count = false, bool $clear = false): void
    {
        if ($this->finished) {
            return;
        }

        // A connection failure can leave the update's outcome unknown.
        // Never finalize the same slot twice. A stranded slot expires with its window.
        $this->finished = true;
        ($this->finish)($count, $clear);
    }

    private function __clone() {}
}
