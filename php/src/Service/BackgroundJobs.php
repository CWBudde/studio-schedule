<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateInterval;
use Doctrine\DBAL\Connection;
use Psr\Clock\ClockInterface;
use Psr\Log\LoggerInterface;
use SensitiveParameter;
use StudioSchedule\Mail\Mailer;
use StudioSchedule\Receipt\AttachmentStore;
use StudioSchedule\Receipt\Attachments;
use StudioSchedule\Store\JobLocks;
use StudioSchedule\Store\JobRunStore;
use StudioSchedule\Store\OutboxStore;
use StudioSchedule\Store\SessionStore;

final readonly class BackgroundJobs
{
    public const string DELIVER_MAIL = 'deliver-mail';
    public const string PURGE_SESSIONS = 'purge-sessions';

    private JobRunner $runner;

    public function __construct(
        private Connection $database,
        #[SensitiveParameter]
        string $dsn,
        private ClockInterface $clock,
        LoggerInterface $logger,
    ) {
        $this->runner = new JobRunner(new JobLocks($dsn), new JobRunStore($database), $clock, $logger);
    }

    public function purgeSessions(): ?JobResult
    {
        return $this->runner->runDue(self::PURGE_SESSIONS, new DateInterval('PT1H'), function (): void {
            (new SessionStore($this->database))->purgeExpired($this->clock->now());
        });
    }

    public function deliverMail(Mailer $mailer, bool $immediate = false): ?JobResult
    {
        $work = function () use ($mailer): void {
            $attachments = new Attachments(new AttachmentStore($this->database));
            (new OutboxDelivery(new OutboxStore($this->database), $this->clock, $mailer, $attachments->find(...)))->deliver();
        };

        return $immediate
            ? $this->runner->runNow(self::DELIVER_MAIL, $work)
            : $this->runner->runDue(self::DELIVER_MAIL, new DateInterval('PT1M'), $work);
    }
}
