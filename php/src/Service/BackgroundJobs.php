<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateInterval;
use Doctrine\DBAL\Connection;
use Psr\Clock\ClockInterface;
use Psr\Log\LoggerInterface;
use SensitiveParameter;
use StudioSchedule\Mail\Mailer;
use StudioSchedule\Mail\Renderer;
use StudioSchedule\Push\Sender;
use StudioSchedule\Receipt\AttachmentStore;
use StudioSchedule\Receipt\Attachments;
use StudioSchedule\Store\AnnouncementRecipients;
use StudioSchedule\Store\AnnouncementStore;
use StudioSchedule\Store\EventStore;
use StudioSchedule\Store\EventWaitlistStore;
use StudioSchedule\Store\JobLocks;
use StudioSchedule\Store\JobRunStore;
use StudioSchedule\Store\MetricsStore;
use StudioSchedule\Store\OutboxStore;
use StudioSchedule\Store\PushOutboxStore;
use StudioSchedule\Store\RetentionStore;
use StudioSchedule\Store\SessionStore;
use StudioSchedule\Store\TimetableStore;

final readonly class BackgroundJobs
{
    public const string GENERATE_LESSONS = 'generate-lessons';
    public const string WAITLIST_EXPIRY = 'waitlist-expiry';
    public const string ANNOUNCEMENTS = 'announcements';
    public const string DELIVER_MAIL = 'deliver-mail';
    public const string DELIVER_PUSH = 'deliver-push';
    public const string PURGE_SESSIONS = 'purge-sessions';
    public const string PURGE_MAIL = 'purge-mail';
    public const string PURGE_PERSONAL_DATA = 'purge-personal-data';

    private JobRunner $runner;

    public function __construct(
        private Connection $database,
        #[SensitiveParameter]
        string $dsn,
        private ClockInterface $clock,
        private LoggerInterface $logger,
        bool $collectMetrics = false,
    ) {
        $metrics = $collectMetrics ? new MetricsStore($dsn, $logger) : null;
        $this->runner = new JobRunner(new JobLocks($dsn), new JobRunStore($database), $clock, $logger, $metrics);
    }

    public function purgeSessions(): ?JobResult
    {
        return $this->runner->runDue(self::PURGE_SESSIONS, new DateInterval('PT1H'), function (): void {
            (new SessionStore($this->database))->purgeExpired($this->clock->now());
        });
    }

    public function generateLessons(): ?JobResult
    {
        return $this->runner->runDue(self::GENERATE_LESSONS, new DateInterval('PT1H'), function (): void {
            $generator = new LessonGenerator(new TimetableStore($this->database), $this->clock);
            try {
                $created = $generator->generateAhead();
            } catch (SettingsNotFound) {
                return;
            }
            if ($created > 0) {
                $this->logger->info('lessons generated', ['created' => $created]);
            }
        });
    }

    public function expireEventInvites(string $baseURL, bool $pushEnabled = false): ?JobResult
    {
        return $this->runner->runDue(self::WAITLIST_EXPIRY, new DateInterval('PT1M'), function () use ($baseURL, $pushEnabled): void {
            $waitlist = new EventWaitlist(
                new EventStore($this->database),
                new EventWaitlistStore($this->database),
                $this->clock,
                new Renderer(),
                $baseURL,
                $pushEnabled,
            );
            $expired = $waitlist->expireDue();
            if ($expired > 0) {
                $this->logger->info('event waiting lists settled', ['entries' => $expired]);
            }
        });
    }

    public function purgeMail(): ?JobResult
    {
        return $this->runner->runDue(self::PURGE_MAIL, new DateInterval('PT24H'), function (): void {
            $counts = (new OutboxStore($this->database))->purge($this->clock->now());
            if (array_sum($counts) > 0) {
                $this->logger->info('old mails purged', $counts);
            }
        });
    }

    public function purgePersonalData(): ?JobResult
    {
        return $this->runner->runDue(self::PURGE_PERSONAL_DATA, new DateInterval('PT24H'), function (): void {
            $counts = (new RetentionStore($this->database))->purge($this->clock->now());
            if (array_sum($counts) > 0) {
                $this->logger->info('personal data purged', $counts);
            }
        });
    }

    public function queueAnnouncements(bool $immediate = false): ?JobResult
    {
        $work = function (): void {
            $announcements = new Announcements(
                new AnnouncementStore($this->database),
                new AnnouncementRecipients($this->database),
                new OutboxStore($this->database),
                new Renderer(),
                $this->clock,
            );
            $queued = $announcements->queueDue();
            if ($queued > 0) {
                $this->logger->info('announcement mails queued', ['queued' => $queued]);
            }
        };

        return $immediate
            ? $this->runner->runNow(self::ANNOUNCEMENTS, $work)
            : $this->runner->runDue(self::ANNOUNCEMENTS, new DateInterval('PT1M'), $work);
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

    public function deliverPush(Sender $sender): ?JobResult
    {
        return $this->runner->runDue(self::DELIVER_PUSH, new DateInterval('PT1M'), function () use ($sender): void {
            $sent = (new PushDelivery(new PushOutboxStore($this->database), $this->clock, $sender))->deliver();
            if ($sent > 0) {
                $this->logger->info('push messages sent', ['sent' => $sent]);
            }
        });
    }
}
