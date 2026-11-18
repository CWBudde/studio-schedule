<?php

declare(strict_types=1);

namespace StudioSchedule\Console;

use Closure;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use InvalidArgumentException;
use Psr\Log\NullLogger;
use SensitiveParameter;
use StudioSchedule\Mail\SmtpMailer;
use StudioSchedule\Service\BackgroundJobs;
use StudioSchedule\Service\JobResult;
use StudioSchedule\Service\SystemClock;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Schema;

final readonly class JobsCommand
{
    public function __construct(private string $postgresMigrations, private string $mysqlMigrations) {}

    /**
     * @param Closure(string):string $environment
     * @param resource $output
     * @param resource $errors
     */
    public function run(#[SensitiveParameter] string $dsn, #[SensitiveParameter] Closure $environment, $output, $errors): int
    {
        if ($dsn === '') {
            throw new CommandError('no database URL: set DATABASE_URL or --database-url', 2);
        }

        try {
            $database = Database::connect($dsn);
        } catch (InvalidArgumentException) {
            throw new CommandError('DATABASE_URL must name PostgreSQL, MySQL or MariaDB', 2);
        }

        try {
            $schema = new Schema($database);
            $expected = Schema::migrationVersions(
                $database->getDatabasePlatform() instanceof PostgreSQLPlatform ? $this->postgresMigrations : $this->mysqlMigrations,
            );
            if ($expected === [] || !in_array('goose_db_version', $schema->relations(), true) || $schema->appliedVersions() !== $expected) {
                throw new CommandError('database schema is not up to date: run studio-schedule migrate status');
            }

            try {
                $mailer = SmtpMailer::fromEnvironment($environment);
            } catch (InvalidArgumentException $error) {
                throw new CommandError($error->getMessage(), 2);
            }

            $jobs = new BackgroundJobs($database, $dsn, new SystemClock(), new NullLogger());
            $plan = [BackgroundJobs::PURGE_SESSIONS => $jobs->purgeSessions(...)];
            if ($mailer !== null) {
                $plan[BackgroundJobs::DELIVER_MAIL] = static fn(): ?JobResult => $jobs->deliverMail($mailer);
            }
            $status = 0;
            foreach ($plan as $name => $run) {
                $result = $run();
                if ($result === JobResult::Failed) {
                    fwrite($errors, "error: $name failed\n");
                    $status = 1;
                } elseif ($result !== null) {
                    fwrite($output, "$name {$result->value}\n");
                }
            }

            return $status;
        } finally {
            $database->close();
        }
    }
}
