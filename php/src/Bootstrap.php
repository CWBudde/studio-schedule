<?php

declare(strict_types=1);

namespace StudioSchedule;

use Monolog\Formatter\JsonFormatter;
use Monolog\Handler\StreamHandler;
use Monolog\Level;
use Monolog\Logger;
use Psr\Clock\ClockInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use Slim\Psr7\Factory\ServerRequestFactory;
use StudioSchedule\Http\DuplicateRoutes;
use StudioSchedule\Http\AccountAdministrationRoutes;
use StudioSchedule\Http\AccountRegistrationEndpoint;
use StudioSchedule\Http\AdminAccountsEndpoint;
use StudioSchedule\Http\AdminAuditEndpoint;
use StudioSchedule\Http\AdminSettingsRoutes;
use StudioSchedule\Http\AfterResponse;
use StudioSchedule\Http\AnnouncementRoutes;
use StudioSchedule\Http\Application;
use StudioSchedule\Http\AuthMailLimit;
use StudioSchedule\Http\AuthSessionRoutes;
use StudioSchedule\Http\CalendarFeedDownload;
use StudioSchedule\Http\CalendarFeedRoutes;
use StudioSchedule\Http\CatalogAdminRoutes;
use StudioSchedule\Http\CatalogRoutes;
use StudioSchedule\Http\CredentialRoutes;
use StudioSchedule\Http\EventRegistrationRoutes;
use StudioSchedule\Http\EventRoutes;
use StudioSchedule\Http\LessonAdminRoutes;
use StudioSchedule\Http\LessonNoticeRoutes;
use StudioSchedule\Http\LessonSignupRoutes;
use StudioSchedule\Http\FamilyRoutes;
use StudioSchedule\Http\Installer;
use StudioSchedule\Http\MetricsEndpoint;
use StudioSchedule\Http\MailLogRoutes;
use StudioSchedule\Http\MeEndpoint;
use StudioSchedule\Http\MemberImportRoutes;
use StudioSchedule\Http\ParticipantReadRoutes;
use StudioSchedule\Http\AttendanceRoutes;
use StudioSchedule\Http\PassReadRoutes;
use StudioSchedule\Http\PasswordFailureLimit;
use StudioSchedule\Http\PasswordResetEndpoint;
use StudioSchedule\Http\PasswordResetRequestEndpoint;
use StudioSchedule\Http\PersonDetailEndpoint;
use StudioSchedule\Http\PersonExportRoutes;
use StudioSchedule\Http\PublicSettingsEndpoint;
use StudioSchedule\Http\PushRoutes;
use StudioSchedule\Http\ReceiptPdfRoutes;
use StudioSchedule\Http\ReceiptReadRoutes;
use StudioSchedule\Http\Responses;
use StudioSchedule\Http\ScheduleRoutes;
use StudioSchedule\Http\TeachingScheduleRoutes;
use StudioSchedule\Http\SessionMiddleware;
use StudioSchedule\Http\StatisticsEndpoint;
use StudioSchedule\Http\WaitlistInviteEndpoint;
use StudioSchedule\Mail\Renderer;
use StudioSchedule\Mail\SmtpMailer;
use StudioSchedule\Push\Sender;
use StudioSchedule\Receipt\AttachmentStore;
use StudioSchedule\Receipt\Attachments;
use StudioSchedule\Service\EventCancellations;
use StudioSchedule\Service\EventPayments;
use StudioSchedule\Service\LessonAdministration;
use StudioSchedule\Service\LessonRecipients;
use StudioSchedule\Service\LessonNotices;
use StudioSchedule\Service\LessonSignup;
use StudioSchedule\Service\LessonWaitlistOffers;
use StudioSchedule\Service\EventRegistrationReads;
use StudioSchedule\Service\ReceiptIssuance;
use StudioSchedule\Service\EventRegistrationWrites;
use StudioSchedule\Service\EventWaitlist;
use StudioSchedule\Service\AccountAdministration;
use StudioSchedule\Service\AccountConfirmation;
use StudioSchedule\Service\AccountRegistration;
use StudioSchedule\Service\AdminSettings;
use StudioSchedule\Service\Announcements;
use StudioSchedule\Service\BackgroundJobs;
use StudioSchedule\Service\BookingRuleSettings;
use StudioSchedule\Service\ClassAdmin;
use StudioSchedule\Service\CalendarFeedManagement;
use StudioSchedule\Service\CalendarFeedRendering;
use StudioSchedule\Service\CommonPasswords;
use StudioSchedule\Service\CachedReadiness;
use StudioSchedule\Service\DatabaseReadiness;
use StudioSchedule\Service\EmailChange;
use StudioSchedule\Service\Family;
use StudioSchedule\Service\Impersonation;
use StudioSchedule\Service\JobResult;
use StudioSchedule\Service\LessonGenerator;
use StudioSchedule\Service\MailLog;
use StudioSchedule\Service\OneOffLesson;
use StudioSchedule\Service\ScheduleRecipients;
use StudioSchedule\Service\TeachingSchedule;
use StudioSchedule\Service\MemberImports;
use StudioSchedule\Service\ParticipantRead;
use StudioSchedule\Service\Attendance;
use StudioSchedule\Service\PassRead;
use StudioSchedule\Service\PasswordAuthentication;
use StudioSchedule\Service\PasswordChange;
use StudioSchedule\Service\PasswordHash;
use StudioSchedule\Service\PasswordReset;
use StudioSchedule\Service\PasswordResetRequests;
use StudioSchedule\Service\PersonDuplicates;
use StudioSchedule\Service\PersonExport;
use StudioSchedule\Service\PublicSettings;
use StudioSchedule\Service\SessionAuthentication;
use StudioSchedule\Service\StatisticsService;
use StudioSchedule\Service\SystemClock;
use StudioSchedule\Service\TermAdmin;
use StudioSchedule\Service\WaitlistInvites;
use StudioSchedule\Store\AdminAccountsStore;
use StudioSchedule\Store\AdminAuditStore;
use StudioSchedule\Store\AnnouncementRecipients;
use StudioSchedule\Store\AnnouncementStore;
use StudioSchedule\Store\CalendarFeedStore;
use StudioSchedule\Store\CatalogStore;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\MetricsStore;
use StudioSchedule\Store\DatabaseCopy;
use StudioSchedule\Store\DuplicateStore;
use StudioSchedule\Store\EmailChangeStore;
use StudioSchedule\Store\EventRegistrationStore;
use StudioSchedule\Store\EventStore;
use StudioSchedule\Store\EventWaitlistStore;
use StudioSchedule\Store\LessonWriteStore;
use StudioSchedule\Store\LessonNoticeStore;
use StudioSchedule\Store\FamilyStore;
use StudioSchedule\Store\MailLogStore;
use StudioSchedule\Store\MemberImportStore;
use StudioSchedule\Store\ParticipantReadStore;
use StudioSchedule\Store\AttendanceStore;
use StudioSchedule\Store\PassReadStore;
use StudioSchedule\Store\OutboxStore;
use StudioSchedule\Store\PasswordResetStore;
use StudioSchedule\Store\PersonDetailStore;
use StudioSchedule\Store\PersonExportStore;
use StudioSchedule\Store\PublicSettingsStore;
use StudioSchedule\Store\PushSubscriptionStore;
use StudioSchedule\Store\RateLimitStore;
use StudioSchedule\Store\ReceiptReadStore;
use StudioSchedule\Store\ReceiptWriteStore;
use StudioSchedule\Store\RegistrationStore;
use StudioSchedule\Store\ScheduleStore;
use StudioSchedule\Store\Schema;
use StudioSchedule\Store\SessionStore;
use StudioSchedule\Store\StatisticsStore;
use StudioSchedule\Store\TimetableStore;
use StudioSchedule\Store\WaitlistInviteStore;
use Symfony\Component\Uid\Uuid;
use Throwable;

final class Bootstrap
{
    /**
     * @param callable():ServerRequestInterface $makeRequest
     * @param (callable(string): ?string)|null $environment
     */
    public static function handle(
        callable $makeRequest,
        string $appDirectory,
        string $publicDirectory,
        string $postgresMigrations,
        ?callable $environment = null,
        ?string $commonPasswordFile = null,
        ?ClockInterface $clock = null,
        ?AfterResponse $afterResponse = null,
    ): ResponseInterface {
        $request = null;
        $connection = null;
        try {
            $request = $makeRequest();
            $clock ??= new SystemClock();
            $config = Config::load($appDirectory . '/config/.env', $environment);
            $logger = new Logger('studio-schedule');
            $level = match ($config->logLevel) {
                'debug' => Level::Debug,
                'warn' => Level::Warning,
                'error' => Level::Error,
                default => Level::Info,
            };
            $handler = new StreamHandler('php://stderr', $level);
            if ($config->logFormat === 'json') {
                $handler->setFormatter(new JsonFormatter());
            }
            $logger->pushHandler($handler);

            $setup = rawurldecode($request->getUri()->getPath()) === $config->basePath . '/setup';
            $connection = $setup || $config->databaseURL === '' ? null : Database::connect($config->databaseURL);
            $schema = $connection === null ? null : new Schema($connection);
            $scheme = parse_url($config->databaseURL, PHP_URL_SCHEME);
            $postgres = is_string($scheme) && in_array(strtolower($scheme), ['postgresql', 'postgres'], true);
            $mysqlMigrations = $appDirectory . '/migrations/mysql';
            $migrations = $postgres ? $postgresMigrations : $mysqlMigrations;
            $expectedVersions = Schema::migrationVersions($migrations);
            $readiness = new DatabaseReadiness($schema, $expectedVersions, $logger);
            $metrics = null;
            $observeRequest = null;
            if ($config->metricsToken !== '') {
                $store = $config->databaseURL === '' ? null : new MetricsStore($config->databaseURL, $logger);
                $versionFile = $appDirectory . '/version.txt';
                $version = is_file($versionFile) ? file_get_contents($versionFile) : false;
                $metrics = new MetricsEndpoint($config->metricsToken, $store, trim($version ?: 'dev'), $logger);
                if ($store !== null) {
                    $observeRequest = static function (string $method, string $route, int $status, int $duration) use ($store, $afterResponse): void {
                        if ($afterResponse === null) {
                            $store->observeRequest($method, $route, $status, $duration);
                        } else {
                            $afterResponse->wake('record-http-metrics', static fn() => $store->observeRequest($method, $route, $status, $duration));
                        }
                    };
                }
            }


            $wakeMail = null;
            $wakeAnnouncements = null;
            if ($afterResponse !== null) {
                $wake = static function (bool $announcements) use ($afterResponse, $appDirectory, $environment, $config, $expectedVersions, $clock, $logger): void {
                    $name = $announcements ? BackgroundJobs::ANNOUNCEMENTS : BackgroundJobs::DELIVER_MAIL;
                    $afterResponse->wake($name, static function () use ($announcements, $appDirectory, $environment, $config, $expectedVersions, $clock, $logger): void {
                        $lookup = Config::lookup($appDirectory . '/config/.env', $environment);
                        $mailer = $announcements ? null : SmtpMailer::fromEnvironment($lookup);
                        if (!$announcements && $mailer === null) {
                            return;
                        }

                        $database = Database::connect($config->databaseURL, dedicated: true);
                        try {
                            if (!(new DatabaseReadiness(new Schema($database), $expectedVersions, $logger))->ready()) {
                                return;
                            }
                            $jobs = new BackgroundJobs($database, $config->databaseURL, $clock, $logger, collectMetrics: $config->metricsToken !== '');
                            if ($announcements) {
                                if ($jobs->queueAnnouncements(immediate: true) !== JobResult::Ok) {
                                    return;
                                }
                                $mailer = SmtpMailer::fromEnvironment($lookup);
                            }
                            if ($mailer !== null) {
                                $jobs->deliverMail($mailer, immediate: true);
                            }
                        } finally {
                            $database->close();
                        }
                    });
                };
                $wakeMail = static fn() => $wake(false);
                $wakeAnnouncements = static fn() => $wake(true);
            }

            $installer = $config->setupToken === ''
                ? null
                : new Installer($config, $migrations, $logger, new DatabaseCopy($postgresMigrations, $mysqlMigrations));

            $commonPasswordFile ??= is_file($appDirectory . '/common-passwords.txt')
                ? $appDirectory . '/common-passwords.txt'
                : dirname($appDirectory) . '/internal/auth/password/common-passwords.txt';
            $resetPassword = $connection === null ? null : static function (
                ServerRequestInterface $request,
                ResponseInterface $response,
            ) use ($connection, $commonPasswordFile, $clock, $wakeMail): ResponseInterface {
                $reset = new PasswordReset(
                    new PasswordResetStore($connection),
                    new PasswordHash(),
                    new CommonPasswords($commonPasswordFile),
                    $clock,
                    new Renderer(),
                    $wakeMail,
                );
                return (new PasswordResetEndpoint($reset))($request, $response);
            };

            $sessions = null;
            $registerRoutes = null;
            if ($connection !== null) {
                $sessionStore = new SessionStore($connection);
                $sessions = new SessionMiddleware($config, new SessionAuthentication($sessionStore, $clock));
                $pushSender = static fn(): ?Sender => Sender::fromEnvironment(Config::lookup($appDirectory . '/config/.env', $environment), $clock);
                $applicationURL = static fn(): string => Config::applicationURL(
                    $config->baseURL,
                    Config::lookup($appDirectory . '/config/.env', $environment)('HTTP_ADDR'),
                );
                $registerRoutes = static function (App $app) use ($sessionStore, $connection, $clock, $config, $logger, $commonPasswordFile, $wakeMail, $wakeAnnouncements, $pushSender, $applicationURL): void {
                    $profile = new MeEndpoint($sessionStore);
                    $limits = new RateLimitStore($connection, $clock, $logger);
                    $mailLimit = new AuthMailLimit($config->trustedProxies, $limits);
                    $failures = new PasswordFailureLimit($limits);
                    $passwords = new PasswordHash();
                    $commonPasswords = new CommonPasswords($commonPasswordFile);
                    $resetStore = new PasswordResetStore($connection);
                    $mail = new Renderer();
                    $app->get('/api/v1/auth/me', $profile);
                    $app->put('/api/v1/auth/locale', $profile->updateLocale(...));
                    $adminAccounts = new AdminAccountsStore($connection);
                    $app->get('/api/v1/admin/accounts', new AdminAccountsEndpoint($adminAccounts));
                    (new AccountAdministrationRoutes(
                        new AccountAdministration($adminAccounts, $sessionStore, $resetStore, $passwords, $mail, new OutboxStore($connection), $clock, $wakeMail),
                        $sessionStore,
                        $failures,
                        $config,
                    ))->register($app);
                    $app->get('/api/v1/admin/audit', new AdminAuditEndpoint(new AdminAuditStore($connection)));
                    $app->post('/api/v1/auth/register', new AccountRegistrationEndpoint(
                        new AccountRegistration(new RegistrationStore($connection), $passwords, $commonPasswords, $clock, $mail, $config->baseURL, $wakeMail),
                        $mailLimit,
                        $config->baseURL,
                    ));
                    $app->post('/api/v1/auth/password-reset', new PasswordResetRequestEndpoint(
                        $config,
                        new PasswordResetRequests($resetStore, $clock, $mail, $config->baseURL, $wakeMail),
                        $mailLimit,
                    ));
                    (new AuthSessionRoutes(
                        $config,
                        new PasswordAuthentication($sessionStore, $passwords, $clock),
                        new AccountConfirmation($sessionStore, $passwords, $clock),
                        new Impersonation($sessionStore, $clock),
                        $profile,
                        $limits,
                        $failures,
                        $logger,
                    ))->register($app);
                    (new CredentialRoutes(
                        new PasswordChange($resetStore, $passwords, $commonPasswords, $clock, $mail, $wakeMail),
                        new EmailChange(new EmailChangeStore($connection), $passwords, $clock, $mail, $config->baseURL, $wakeMail),
                        $sessionStore,
                        $failures,
                        $mailLimit,
                        $config->baseURL,
                    ))->register($app);
                    $settingsStore = new PublicSettingsStore($connection);
                    $schedule = new ScheduleStore($connection);
                    (new PublicSettingsEndpoint(new PublicSettings($settingsStore)))->register($app);
                    $timetableStore = new TimetableStore($connection);
                    $generator = new LessonGenerator($timetableStore, $clock);
                    (new AdminSettingsRoutes(new AdminSettings($settingsStore), new BookingRuleSettings($schedule, $clock, $generator, $logger)))->register($app);
                    $catalog = new CatalogStore($connection);
                    CatalogRoutes::register($app, $catalog, $clock);
                    $memberDirectory = new MemberImportStore($connection);
                    (new MemberImportRoutes(new MemberImports($memberDirectory, $clock)))->register($app);
                    CatalogAdminRoutes::registerTerms($app, new TermAdmin($timetableStore, $generator, $clock));
                    CatalogAdminRoutes::registerClasses($app, new ClassAdmin($timetableStore, $generator, $clock));
                    $lessons = new OneOffLesson($timetableStore, $clock);
                    CatalogAdminRoutes::registerLessonCreation($app, $lessons);
                    $eventStore = new EventStore($connection);
                    $registrationStore = new EventRegistrationStore($connection);
                    $eventRegistrations = new EventRegistrationReads($eventStore, $registrationStore, $clock);
                    $lessonRecipients = new EventWaitlistStore($connection);
                    $lessonWrites = new LessonWriteStore($connection);
                    $lessonParticipants = new LessonRecipients($lessonWrites, $registrationStore, new ScheduleRecipients($lessonRecipients));
                    (new LessonNoticeRoutes(new LessonNotices(
                        new LessonNoticeStore($connection, $eventStore, $lessonWrites, $schedule),
                        $lessonRecipients,
                        $lessonParticipants,
                        $mail,
                        $clock,
                        $wakeMail,
                    )))->register($app);
                    $lessonOffers = new LessonWaitlistOffers(
                        $schedule,
                        $lessonWrites,
                        $lessonRecipients,
                        $mail,
                        $applicationURL,
                        static fn(): bool => $pushSender() !== null,
                    );
                    (new LessonAdminRoutes(new LessonAdministration(
                        $eventStore,
                        $lessonWrites,
                        $schedule,
                        $lessonParticipants,
                        $lessonRecipients,
                        $timetableStore,
                        $lessonOffers,
                        $mail,
                        $clock,
                        $wakeMail,
                    )))->register($app);
                    $eventWaitlist = static fn(): EventWaitlist => new EventWaitlist(
                        $eventStore,
                        $lessonRecipients,
                        $clock,
                        $mail,
                        $applicationURL(),
                        $pushSender() !== null,
                    );
                    $eventReceipts = new ReceiptIssuance(new ReceiptWriteStore($connection), $mail);
                    $eventCancellations = new EventCancellations($eventStore, $registrationStore, $eventReceipts, $clock, $eventWaitlist, $wakeMail);
                    EventRoutes::register($app, $eventStore, $clock, $lessons, $eventWaitlist, $eventCancellations);
                    (new EventRegistrationRoutes(
                        $eventRegistrations,
                        static fn(): EventRegistrationWrites => new EventRegistrationWrites($eventStore, $registrationStore, $eventWaitlist(), $clock),
                        new EventPayments($eventStore, $registrationStore, $eventReceipts, $clock, $wakeMail),
                        $eventCancellations,
                    ))->register($app);
                    $familyStore = new FamilyStore($connection);
                    $duplicates = new DuplicateStore($connection, $memberDirectory, $familyStore, $clock);
                    (new DuplicateRoutes(new PersonDuplicates($duplicates)))->register($app);
                    (new FamilyRoutes(new Family($familyStore, $clock)))->register($app);
                    $personExport = new PersonExportStore($connection);
                    (new PersonExportRoutes(new PersonExport($personExport, $familyStore, $clock)))->register($app);
                    $app->get('/api/v1/admin/persons/{personId}', new PersonDetailEndpoint(new PersonDetailStore($connection, $personExport, $catalog)))
                        ->add(CatalogAdminRoutes::requireOffice(...));
                    (new AnnouncementRoutes(new Announcements(
                        new AnnouncementStore($connection),
                        new AnnouncementRecipients($connection),
                        new OutboxStore($connection),
                        $mail,
                        $clock,
                        $wakeAnnouncements,
                    )))->register($app);
                    (new MailLogRoutes(new MailLog(new MailLogStore($connection), $clock)))->register($app);
                    $participants = new ParticipantReadStore($connection);
                    (new AttendanceRoutes(new Attendance(
                        new AttendanceStore($connection, $eventStore, $lessonWrites, $participants),
                        $clock,
                    )))->register($app);
                    (new ParticipantReadRoutes(new ParticipantRead($participants, $clock)))->register($app);
                    (new TeachingScheduleRoutes(new TeachingSchedule($schedule, $clock)))->register($app);
                    (new LessonSignupRoutes(new LessonSignup($schedule, $participants, $familyStore, $clock)))->register($app);
                    (new PassReadRoutes(new PassRead(new PassReadStore($connection, $schedule), $clock)))->register($app);
                    (new ReceiptPdfRoutes(new Attachments(new AttachmentStore($connection))))->register($app);
                    (new ReceiptReadRoutes(new ReceiptReadStore($connection, $clock)))->register($app);
                    ScheduleRoutes::register($app, $schedule, $clock);
                    $app->get('/api/v1/waitlist-invites/{token}', new WaitlistInviteEndpoint(
                        (new WaitlistInvites(new WaitlistInviteStore($connection, $schedule), $clock))->view(...),
                        $config->trustedProxies,
                        $limits,
                    ));
                    $app->get('/api/v1/event-invites/{token}', new WaitlistInviteEndpoint(
                        $eventRegistrations->invite(...),
                        $config->trustedProxies,
                        $limits,
                    ));
                    $app->post('/api/v1/event-invites/{token}/accept', new WaitlistInviteEndpoint(
                        static fn(#[SensitiveParameter] string $token): ?true => $eventWaitlist()->respondByToken($token, accept: true),
                        $config->trustedProxies,
                        $limits,
                    ));
                    $app->post('/api/v1/event-invites/{token}/decline', new WaitlistInviteEndpoint(
                        static fn(#[SensitiveParameter] string $token): ?true => $eventWaitlist()->respondByToken($token, accept: false),
                        $config->trustedProxies,
                        $limits,
                    ));
                    $app->get('/api/v1/admin/statistics', new StatisticsEndpoint(new StatisticsService(new StatisticsStore($connection), $schedule, $clock)));
                    $pushRoutes = new PushRoutes(new PushSubscriptionStore($connection), $clock, $pushSender);
                    $app->get('/api/v1/my/push', $pushRoutes);
                    $app->map(['POST', 'DELETE'], '/api/v1/my/push/subscriptions', $pushRoutes);
                    $calendarFeeds = new CalendarFeedStore($connection);
                    (new CalendarFeedRoutes(
                        new CalendarFeedManagement($calendarFeeds, $clock),
                        $config->basePath,
                        $config->baseURL,
                    ))->register($app);
                    $app->get('/ical/{token:[^/.]+}.ics', new CalendarFeedDownload(
                        new CalendarFeedRendering($calendarFeeds, $schedule, $clock, $config->baseURL),
                        $limits,
                        $config->trustedProxies,
                    ));
                };
            }

            return Application::create(
                $config,
                $publicDirectory,
                $readiness,
                $logger,
                $installer,
                $resetPassword,
                $sessions,
                $registerRoutes,
                new CachedReadiness($config->databaseURL, $expectedVersions, $clock, $logger),
                $metrics,
                $observeRequest,
            )->handle($request);
        } catch (Throwable $error) {
            $id = Uuid::v7()->toRfc4122();
            error_log('studio-schedule startup error ' . $id . ': ' . $error::class);
            $request ??= (new ServerRequestFactory())->createServerRequest('GET', '/');
            $response = Responses::problem($request->withAttribute('requestId', $id), 500, 'internal server error');

            return Responses::secure($response, $id);
        } finally {
            $connection?->close();
        }
    }
}
