<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use StudioSchedule\Store\MetricsStore;

/** @phpstan-import-type MetricSample from MetricsStore */
final class PrometheusMetrics
{
    private const string NAMESPACE = 'studio_schedule_';

    /**
     * @param list<MetricSample> $samples
     * @param array{pending:int,failed:int}|null $outbox
     */
    public static function render(array $samples, ?array $outbox, string $version): string
    {
        $requestCounts = $requestDurations = $jobCounts = $jobDurations = $jobLastSuccess = [];
        foreach ($samples as $sample) {
            $count = (int) $sample['observations'];
            $duration = (int) $sample['duration_us'];
            $bucket = (int) $sample['bucket'];
            if ($sample['family'] === 'http') {
                $labels = 'method=' . self::quote($sample['label']) . ',route=' . self::quote($sample['name']);
                $counter = $labels . ',status=' . self::quote((string) $sample['status']);
                $requestCounts[$counter] = ($requestCounts[$counter] ?? 0) + $count;
                $requestDurations[$labels]['buckets'][$bucket] = ($requestDurations[$labels]['buckets'][$bucket] ?? 0) + $count;
                $requestDurations[$labels]['duration'] = ($requestDurations[$labels]['duration'] ?? 0) + $duration;
            } else {
                $labels = 'job=' . self::quote($sample['name']);
                $counter = $labels . ',result=' . self::quote($sample['label']);
                $jobCounts[$counter] = ($jobCounts[$counter] ?? 0) + $count;
                $jobDurations[$labels]['buckets'][$bucket] = ($jobDurations[$labels]['buckets'][$bucket] ?? 0) + $count;
                $jobDurations[$labels]['duration'] = ($jobDurations[$labels]['duration'] ?? 0) + $duration;
                if ($sample['label'] === 'success') {
                    $jobLastSuccess[$labels] = max($jobLastSuccess[$labels] ?? 0, (int) $sample['last_success_us']);
                }
            }
        }

        $output = self::counts('http_requests_total', 'counter', 'HTTP requests answered, by method, route pattern and status code.', $requestCounts);
        $output .= self::histograms('http_request_duration_seconds', 'Time to answer HTTP requests, by method and route pattern.', $requestDurations);
        $output .= self::counts('job_runs_total', 'counter', 'Runs of background jobs, by job and result (success or failure).', $jobCounts);
        $output .= self::histograms('job_duration_seconds', 'Duration of background job runs, by job.', $jobDurations);
        $output .= self::counts(
            'job_last_success_timestamp_seconds',
            'gauge',
            'Unix time of the last successful run of a background job.',
            array_map(self::seconds(...), $jobLastSuccess),
        );
        if ($outbox !== null) {
            $output .= self::counts('outbox_mails', 'gauge', 'Mails in the outbox that are not sent, by status: pending (another attempt is due) or failed (delivery was given up).', [
                'status="pending"' => $outbox['pending'],
                'status="failed"' => $outbox['failed'],
            ]);
        }

        return $output . self::counts('build_info', 'gauge', 'Always 1; the version label names the running build.', ['version=' . self::quote($version) => 1]);
    }

    /** @param array<string,int|string> $series */
    private static function counts(string $name, string $type, string $help, array $series): string
    {
        if ($series === []) {
            return '';
        }

        $output = self::metadata($name, $type, $help);
        foreach ($series as $labels => $value) {
            $output .= self::sample($name, $labels, $value);
        }

        return $output;
    }

    /** @param array<string,array{buckets:array<int,int>,duration:int}> $series */
    private static function histograms(string $name, string $help, array $series): string
    {
        if ($series === []) {
            return '';
        }

        $output = self::metadata($name, 'histogram', $help);
        foreach ($series as $labels => $histogram) {
            $count = 0;
            foreach (MetricBuckets::UPPER_BOUNDS as $index => $bound) {
                $count += $histogram['buckets'][$index] ?? 0;
                $output .= self::sample($name . '_bucket', $labels . ',le=' . self::quote(self::seconds($bound)), $count);
            }

            $count += $histogram['buckets'][count(MetricBuckets::UPPER_BOUNDS)] ?? 0;
            $output .= self::sample($name . '_bucket', $labels . ',le="+Inf"', $count);
            $output .= self::sample($name . '_sum', $labels, self::seconds($histogram['duration']));
            $output .= self::sample($name . '_count', $labels, $count);
        }

        return $output;
    }

    private static function seconds(int $micros): string
    {
        $whole = intdiv($micros, 1_000_000);
        $fraction = $micros % 1_000_000;

        return $fraction === 0 ? (string) $whole : $whole . '.' . rtrim(sprintf('%06d', $fraction), '0');
    }

    private static function metadata(string $name, string $type, string $help): string
    {
        $metric = self::NAMESPACE . $name;

        return "# HELP $metric $help\n# TYPE $metric $type\n";
    }

    private static function quote(string $label): string
    {
        return '"' . strtr($label, ["\\" => "\\\\", '"' => '\\"', "\n" => '\\n']) . '"';
    }

    private static function sample(string $name, string $labels, int|string $value): string
    {
        return self::NAMESPACE . $name . '{' . $labels . "} $value\n";
    }
}
