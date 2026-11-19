<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

final class MetricBuckets
{
    /** Standard Prometheus histogram boundaries, in microseconds. */
    public const array UPPER_BOUNDS = [5_000, 10_000, 25_000, 50_000, 100_000, 250_000, 500_000, 1_000_000, 2_500_000, 5_000_000, 10_000_000];

    public static function bucket(int $micros): int
    {
        foreach (self::UPPER_BOUNDS as $bucket => $bound) {
            if ($micros <= $bound) {
                return $bucket;
            }
        }

        return count(self::UPPER_BOUNDS);
    }
}
