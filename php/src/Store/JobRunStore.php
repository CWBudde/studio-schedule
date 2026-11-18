<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use StudioSchedule\Service\JobResult;
use Symfony\Component\Uid\Uuid;

final readonly class JobRunStore
{
    public function __construct(private Connection $database) {}

    public function ranSince(string $job, DateTimeImmutable $cutoff): bool
    {
        return $this->database->fetchOne(
            "SELECT 1 FROM job_runs WHERE job = ? AND started_at > ? AND result <> 'skipped' LIMIT 1",
            [$job, $this->timestamp($cutoff)],
        ) !== false;
    }

    public function record(string $job, DateTimeImmutable $start, DateTimeImmutable $end, JobResult $result): void
    {
        $id = Uuid::v7();
        $this->database->executeStatement(
            'INSERT INTO job_runs (id, job, started_at, ended_at, result, error) VALUES (?, ?, ?, ?, ?, ?)',
            [
                $this->isPostgres() ? $id->toRfc4122() : $id->toBinary(), $job,
                $this->timestamp($start), $this->timestamp(max($start, $end)), $result->value,
                $result === JobResult::Failed ? 'background job failed' : null,
            ],
        );
    }

    private function timestamp(DateTimeImmutable $at): string
    {
        return $at->setTimezone(new DateTimeZone('UTC'))->format($this->isPostgres() ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u');
    }

    private function isPostgres(): bool
    {
        return $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
