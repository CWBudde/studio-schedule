<?php

declare(strict_types=1);

namespace StudioSchedule\Console;

use Closure;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use InvalidArgumentException;
use Psr\Log\NullLogger;
use SensitiveParameter;
use StudioSchedule\Config;
use StudioSchedule\Mail\SmtpMailer;
use StudioSchedule\Push\Sender;
use StudioSchedule\Service\BackgroundJobs;
use StudioSchedule\Service\JobResult;
use StudioSchedule\Service\SetupInput;
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

            $clock = new SystemClock();
            try {
                $mailer = SmtpMailer::fromEnvironment($environment);
                $pusher = Sender::fromEnvironment($environment, $clock);
                [$baseURL] = Config::parseBaseURL(SetupInput::trim($environment('BASE_URL')));
                $baseURL = Config::applicationURL($baseURL, $environment('HTTP_ADDR'));
            } catch (InvalidArgumentException $error) {
                throw new CommandError($error->getMessage(), 2);
            }

            $jobs = new BackgroundJobs($database, $dsn, $clock, new NullLogger(), collectMetrics: $environment('METRICS_TOKEN') !== '');
            $plan = [
                BackgroundJobs::GENERATE_LESSONS => $jobs->generateLessons(...),
                BackgroundJobs::WAITLIST_EXPIRY => static fn(): ?JobResult => $jobs->expireEventInvites($baseURL, $pusher !== null),
                BackgroundJobs::ANNOUNCEMENTS => $jobs->queueAnnouncements(...),
                BackgroundJobs::PURGE_SESSIONS => $jobs->purgeSessions(...),
                BackgroundJobs::PURGE_MAIL => $jobs->purgeMail(...),
                BackgroundJobs::PURGE_PERSONAL_DATA => $jobs->purgePersonalData(...),
            ];
            if ($mailer !== null) {
                $plan[BackgroundJobs::DELIVER_MAIL] = static fn(): ?JobResult => $jobs->deliverMail($mailer);
            }
            if ($pusher !== null) {
                $plan[BackgroundJobs::DELIVER_PUSH] = static fn(): ?JobResult => $jobs->deliverPush($pusher);
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
