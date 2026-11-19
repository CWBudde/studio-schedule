<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception;
use Psr\Log\LoggerInterface;
use SensitiveParameter;
use StudioSchedule\Service\JobResult;
use StudioSchedule\Service\MetricBuckets;

/**
 * @phpstan-type MetricSample array{
 *     family:string,name:string,label:string,status:int|numeric-string,bucket:int|numeric-string,
 *     observations:int|numeric-string,duration_us:int|numeric-string,last_success_us:int|numeric-string
 * }
 */
final readonly class MetricsStore
{
    public function __construct(
        #[SensitiveParameter]
        private string $databaseURL,
        private LoggerInterface $logger,
    ) {}

    public function observeRequest(string $method, string $route, int $status, int $duration): void
    {
        $method = in_array($method, ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'], true) ? $method : 'OTHER';
        $this->observe('http', $route, $method, $status, $duration, 0);
    }

    public function observeJob(string $name, JobResult $result, int $duration, DateTimeImmutable $end): void
    {
        if ($result === JobResult::Skipped) {
            return;
        }

        $success = $result === JobResult::Ok;
        $lastSuccess = $success ? $end->getTimestamp() * 1_000_000 + (int) $end->format('u') : 0;
        $this->observe('job', $name, $success ? 'success' : 'failure', 0, $duration, $lastSuccess);
    }

    /** @return array{samples:list<MetricSample>,outbox:array{pending:int,failed:int}|null} */
    public function snapshot(bool $includeOutbox): array
    {
        $database = Database::connect($this->databaseURL, dedicated: true, timeoutSeconds: 2);
        try {
            self::limitQueries($database);
            /** @var list<MetricSample> $rows */
            $rows = $database->fetchAllAssociative(
                'SELECT family, name, label, status, bucket, observations, duration_us, last_success_us'
                . ' FROM php_metrics ORDER BY family, name, label, status, bucket',
            );

            return ['samples' => $rows, 'outbox' => $includeOutbox ? $this->outbox($database) : null];
        } finally {
            $database->close();
        }
    }

    /** @return array{pending:int,failed:int}|null */
    private function outbox(Connection $database): ?array
    {
        try {
            /** @var array{pending:int|numeric-string,failed:int|numeric-string} $counts */
            $counts = $database->fetchAssociative(
                'SELECT (SELECT COUNT(*) FROM outbox_mails WHERE sent_at IS NULL AND next_attempt_at IS NOT NULL) AS pending,'
                . ' (SELECT COUNT(*) FROM outbox_mails WHERE sent_at IS NULL AND next_attempt_at IS NULL) AS failed',
            );

            return ['pending' => (int) $counts['pending'], 'failed' => (int) $counts['failed']];
        } catch (Exception $error) {
            $this->logger->warning('metrics: counting the outbox failed', ['exception' => $error::class]);

            return null;
        }
    }

    private function observe(string $family, string $name, string $label, int $status, int $duration, int $lastSuccess): void
    {
        $database = Database::connect($this->databaseURL, dedicated: true, timeoutSeconds: 2);
        try {
            self::limitQueries($database);
            $conflict = ($database->getParams()['driver'] ?? null) === 'pdo_pgsql'
                ? 'ON CONFLICT (family, name, label, status, bucket) DO UPDATE SET'
                : 'ON DUPLICATE KEY UPDATE';
            $database->executeStatement(
                'INSERT INTO php_metrics (family, name, label, status, bucket, observations, duration_us, last_success_us)'
                . " VALUES (?, ?, ?, ?, ?, 1, ?, ?) $conflict observations = php_metrics.observations + 1,"
                . ' duration_us = php_metrics.duration_us + ?, last_success_us = GREATEST(php_metrics.last_success_us, ?)',
                [$family, $name, $label, $status, MetricBuckets::bucket($duration), $duration, $lastSuccess, $duration, $lastSuccess],
            );
        } catch (Exception $error) {
            $this->logger->debug('metrics: recording an observation failed', ['exception' => $error::class]);
        } finally {
            $database->close();
        }
    }

    private static function limitQueries(Connection $database): void
    {
        Database::limitMonitoringLockWait($database);
        if (($database->getParams()['driver'] ?? null) === 'pdo_pgsql') {
            $database->executeStatement("SET statement_timeout = '1s'");
        }
    }
}
