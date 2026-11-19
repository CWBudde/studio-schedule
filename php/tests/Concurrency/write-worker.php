<?php

declare(strict_types=1);

namespace StudioSchedule\Tests\Concurrency;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Configuration;
use Doctrine\DBAL\DriverManager;
use Doctrine\DBAL\Logging\Middleware;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use InvalidArgumentException;
use Psr\Clock\ClockInterface;
use Psr\Log\AbstractLogger;
use RuntimeException;
use Stringable;
use StudioSchedule\Mail\Renderer;
use StudioSchedule\Service\Attendance;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\EventRegistrationWrites;
use StudioSchedule\Service\EventWaitlist;
use StudioSchedule\Service\PassIssuance;
use StudioSchedule\Service\ReceiptIssuance;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Store\AttendanceStore;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\EventRegistrationStore;
use StudioSchedule\Store\EventStore;
use StudioSchedule\Store\EventWaitlistStore;
use StudioSchedule\Store\FamilyStore;
use StudioSchedule\Store\LessonWriteStore;
use StudioSchedule\Store\ParticipantReadStore;
use StudioSchedule\Store\PassIssueStore;
use StudioSchedule\Store\PassReadStore;
use StudioSchedule\Store\ReceiptWriteStore;
use StudioSchedule\Store\ScheduleStore;

require dirname(__DIR__, 2) . '/vendor/autoload.php';

[, $scenario, $actor, $target, $mode] = array_pad($argv, 5, '');
$dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
if ($dsn === false || !in_array($mode, ['commit', 'rollback', 'contender'], true)) {
    throw new InvalidArgumentException('A disposable database and worker mode are required');
}
$proceed = static function (): void {
    echo "holding\n";
    fflush(STDOUT);
    if (fgets(STDIN) !== "release\n") {
        throw new RuntimeException('The test did not release the database write');
    }
};
$logger = new class ($mode === 'commit', $proceed) extends AbstractLogger {
    /** @param Closure():void $hold */
    public function __construct(private readonly bool $pauseAtCommit, private readonly Closure $hold) {}

    /** @param array<string,mixed> $context */
    public function log(mixed $level, string|Stringable $message, array $context = []): void
    {
        if ($this->pauseAtCommit && $message === 'Committing transaction') {
            ($this->hold)();
        }
    }
};
$config = new Configuration();
$config->setMiddlewares([new Middleware($logger)]);
$params = Database::connect($dsn)->getParams();
$db = DriverManager::getConnection($params, $config);
$pid = $db->fetchOne($db->getDatabasePlatform() instanceof PostgreSQLPlatform ? 'SELECT pg_backend_pid()' : 'SELECT CONNECTION_ID()');
if (!is_numeric($pid)) {
    throw new RuntimeException('The database did not report a connection ID');
}
echo $pid, "\n";
fflush(STDOUT);

$instant = match ($scenario) {
    'capacity' => '2026-10-25T16:59:59Z',
    'redemption' => '2026-11-01T12:00:00Z',
    'receipt' => '2026-12-31T23:30:00Z',
    default => throw new InvalidArgumentException('Unknown concurrency test scenario'),
};
$clock = new class (new DateTimeImmutable($instant)) implements ClockInterface {
    public function __construct(private readonly DateTimeImmutable $instant) {}

    public function now(): DateTimeImmutable
    {
        return $this->instant;
    }
};
$events = new EventStore($db);
$mail = new Renderer();

try {
    switch ($scenario) {
        case 'capacity':
            $registrations = new EventRegistrationWrites(
                $events,
                new EventRegistrationStore($db),
                new EventWaitlist($events, new EventWaitlistStore($db), $clock, $mail, 'https://school.example.test/dance', false),
                $clock,
            );
            $registrations->signup(new SessionPrincipal($actor, $target, 'member'), 'winter-workshop', $target, waiting: false);
            break;
        case 'redemption':
            $attendance = new Attendance(new AttendanceStore($db, $events, new LessonWriteStore($db), new ParticipantReadStore($db)), $clock);
            $attendance->set(new SessionPrincipal($actor, '00000000-0000-7000-8000-000000000005', 'office'), $target, 'attended');
            break;
        case 'receipt':
            $render = $mode !== 'rollback' ? null : static function () use ($proceed): never {
                $proceed();
                throw new RuntimeException('Fixture receipt render failed');
            };
            $passes = new PassIssuance(
                new PassIssueStore($db, new FamilyStore($db), new PassReadStore($db, new ScheduleStore($db)), $events),
                new ReceiptIssuance(new ReceiptWriteStore($db), $mail, $render),
                $clock,
            );
            $passes->issue(
                new SessionPrincipal($actor, '00000000-0000-7000-8000-000000000005', 'office'),
                $target,
                '00000000-0000-7000-8000-00000000012d',
                null,
                '',
                ['payerId' => null, 'price' => null, 'method' => 'cash', 'name' => '', 'address' => '', 'email' => 'receipt@example.test'],
            );
            break;
    }

    $outcome = 'ok';
} catch (CatalogFailure $error) {
    $outcome = 'refused ' . $error->reason;
} catch (RuntimeException $error) {
    if ($error->getMessage() !== 'Fixture receipt render failed') {
        throw $error;
    }
    $outcome = 'rolled back';
} finally {
    $db->close();
}
echo $outcome, "\n";
