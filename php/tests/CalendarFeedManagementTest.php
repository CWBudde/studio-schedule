<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Http\Message\ResponseInterface;
use Slim\Psr7\Factory\ServerRequestFactory;
use StudioSchedule\Bootstrap;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;

final class CalendarFeedManagementTest extends TestCase
{
    private const NOW = '2026-12-04 12:00:00';
    private const BASE = 'https://school.example/dance';
    private const FEEDS = '/api/v1/my/calendar-feeds';
    private const MEMBER = '018f0000-0000-7000-8000-000000000001';
    private const ADMIN = '018f0000-0000-7000-8000-000000000002';
    private const LOCATION = '018f0000-0000-7000-8000-000000000003';

    public function testMountedManagementReplacesOnlyTheOwnedSubjectAndStoresOnlyHashes(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database from just php-test');
        }
        $db = Database::connect($dsn);
        self::assertSame([], (new Schema($db))->relations(), 'Requires an empty disposable database');
        $postgres = $db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $pgPath = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations';
        $request = static fn(string $method, string $path = self::FEEDS, string $cookie = 'member-session', string $body = ''): ResponseInterface
            => self::request($dsn, $pgPath, $method, $path, $cookie, $body);
        $location = self::id(self::LOCATION, $postgres);
        $member = self::id(self::MEMBER, $postgres);
        $admin = self::id(self::ADMIN, $postgres);

        try {
            (new Migrator($dsn))->up($postgres ? $pgPath : dirname(__DIR__) . '/migrations/mysql');
            self::seedAccount($db, $postgres, $member, 'member', self::MEMBER);
            self::seedAccount($db, $postgres, $admin, 'admin', self::ADMIN);
            self::seedSession($db, $postgres, $member, 'impersonated-member', self::LOCATION, $admin);
            $db->executeStatement("INSERT INTO locations (id, name, address) VALUES (?, 'North Studio', ?)", [$location, "Fictional Road 1
Town"]);
            self::seedSchedule($db, $postgres, $member, $admin, $location);

            self::assertAttendanceAnnouncement($db, $postgres, $request);

            self::assertSame(401, $request('GET', cookie: '')->getStatusCode());
            self::assertSame(['items' => []], self::json($request('GET'), 200));
            self::assertSame(403, $request('POST', body: '{"scope":"teaching"}')->getStatusCode());
            self::assertSame(403, $request('POST', cookie: 'impersonated-member', body: '{"scope":"bookings"}')->getStatusCode());
            self::assertSame(422, $request('POST', body: '{"scope":"all"}')->getStatusCode());
            self::assertSame(422, $request('POST', body: '{"scope":"schedule","locationId":"not-an-id"}')->getStatusCode());
            self::assertSame(422, $request('POST', body: '{"scope":"bookings","locationId":"' . self::LOCATION . '"}')->getStatusCode());
            self::assertSame(422, $request('POST', body: '{"scope":"schedule","locationId":"' . self::ADMIN . '"}')->getStatusCode());

            $first = self::json($request('POST', body: '{"scope":"bookings"}'), 201);
            $firstId = self::string($first['feed']['id']);
            $firstToken = self::string($first['token']);
            self::assertMatchesRegularExpression('/^[a-zA-Z0-9_-]{43}$/D', $firstToken);
            self::assertSame('/dance/ical/' . $firstToken . '.ics', $first['path']);
            self::assertSame(self::BASE . '/ical/' . $firstToken . '.ics', $first['url'] ?? null);
            self::assertSame('2026-12-04T12:00:00Z', $first['feed']['createdAt']);
            self::assertArrayNotHasKey('lastUsedAt', $first['feed']);
            self::assertArrayNotHasKey('locationId', $first['feed']);
            $hash = $postgres ? "encode(token_hash, 'hex')" : 'LOWER(HEX(token_hash))';
            self::assertSame(hash('sha256', $firstToken), $db->fetchOne("SELECT $hash FROM calendar_feeds"));

            $local = self::json($request('POST', body: '{"scope":"schedule","locationId":"' . self::LOCATION . '"}'), 201);
            self::assertSame(self::LOCATION, $local['feed']['locationId']);
            self::assertSame('North Studio', $local['feed']['locationName']);
            $all = self::json($request('POST', body: '{"scope":"schedule"}'), 201);
            $second = self::json($request('POST', body: '{"scope":"bookings"}'), 201);
            $secondId = self::string($second['feed']['id']);
            self::assertNotSame($firstId, $secondId);
            self::assertNotSame($firstToken, $second['token']);
            self::assertContains($db->fetchOne('SELECT COUNT(*) FROM calendar_feeds WHERE revoked_at IS NOT NULL'), [1, '1']);

            $bookingPath = substr(self::string($second['path']), strlen('/dance'));
            $schedulePath = substr(self::string($local['path']), strlen('/dance'));
            $doc = $request('GET', $bookingPath . '?download=true', cookie: '');
            self::assertSame(200, $doc->getStatusCode(), (string) $doc->getBody());
            self::assertSame('text/calendar; charset=utf-8', $doc->getHeaderLine('Content-Type'));
            self::assertSame('private, max-age=900', $doc->getHeaderLine('Cache-Control'));
            self::assertSame('attachment; filename="fictional-hall-bookings.ics"', $doc->getHeaderLine('Content-Disposition'));
            $body = str_replace("\r\n ", '', (string) $doc->getBody());
            self::assertStringContainsString("NAME:Fictional Hall – My lessons\r\n", $body);
            self::assertStringContainsString("SUMMARY:Canceled: Ballet – Ari Fable\r\n", $body);
            self::assertStringContainsString('DESCRIPTION:Hall unavailable\nBooked: Ari Fable\nInstructor: Ari Fable', $body);
            self::assertStringContainsString("URL;VALUE=URI:https://school.example/dance/schedule?from=2026-12-03&lesson=", $body);
            self::assertStringContainsString("STATUS:CANCELLED\r\n", $body);
            self::assertMatchesRegularExpression('/^"[a-f0-9]{64}"$/D', $doc->getHeaderLine('ETag'));
            $notModified = self::request($dsn, $pgPath, 'GET', $bookingPath, '', '', ['If-None-Match' => 'W/' . $doc->getHeaderLine('ETag')], '2026-12-04 13:00:00');
            self::assertSame(304, $notModified->getStatusCode());
            self::assertSame('', (string) $notModified->getBody());
            $schedule = $request('GET', $schedulePath, cookie: '');
            self::assertSame(200, $schedule->getStatusCode());
            self::assertSame('inline', $schedule->getHeaderLine('Content-Disposition'));
            self::assertStringContainsString('NAME:Fictional Hall – Schedule (North Studio)', (string) $schedule->getBody());
            self::assertStringNotContainsString('BEGIN:VEVENT', (string) $schedule->getBody(), 'Hidden lessons cannot appear in member schedule feeds');

            $privateList = $request('GET', cookie: 'impersonated-member');
            self::assertStringNotContainsString($firstToken, (string) $privateList->getBody());
            self::assertStringNotContainsString(self::string($second['token']), (string) $privateList->getBody());
            $listed = self::json($privateList, 200);
            self::assertSame([$local['feed'] + ['lastUsedAt' => '2026-12-04T12:00:00Z'], $all['feed'], $second['feed'] + ['lastUsedAt' => '2026-12-04T12:00:00Z']], $listed['items']);
            self::assertSame(['items' => []], self::json($request('GET', cookie: 'admin-session'), 200));

            self::assertSame(404, $request('DELETE', self::FEEDS . '/' . $firstId)->getStatusCode());
            self::assertSame(404, $request('DELETE', self::FEEDS . '/' . $secondId, 'admin-session')->getStatusCode());
            self::assertSame(422, $request('DELETE', self::FEEDS . '/invalid-id')->getStatusCode());
            $revoked = $request('DELETE', self::FEEDS . '/' . $secondId, 'impersonated-member');
            self::assertSame(204, $revoked->getStatusCode());
            self::assertSame('', (string) $revoked->getBody());
            self::assertSame(404, $request('DELETE', self::FEEDS . '/' . $secondId)->getStatusCode());
            self::assertSame(404, $request('GET', $bookingPath, cookie: '')->getStatusCode());
            self::assertSame(404, $request('GET', substr(self::string($first['path']), strlen('/dance')), cookie: '')->getStatusCode());
            $db->executeStatement('UPDATE accounts SET disabled_at = ? WHERE id = ?', [self::NOW, $member]);
            self::assertSame(404, $request('GET', $schedulePath, cookie: '')->getStatusCode());
            $db->executeStatement('UPDATE accounts SET disabled_at = NULL WHERE id = ?', [$member]);

            $db->executeStatement("UPDATE staff SET active = TRUE WHERE id = ?", [$admin]);
            $teaching = self::json($request('POST', cookie: 'admin-session', body: '{"scope":"teaching"}'), 201);
            self::assertSame('teaching', $teaching['feed']['scope']);
            $teachingPath = substr(self::string($teaching['path']), strlen('/dance'));
            self::assertStringContainsString('Ballet – Ari Fable (instructor)', (string) $request('GET', $teachingPath, cookie: '')->getBody());
            $db->executeStatement('UPDATE staff SET active = FALSE WHERE id = ?', [$admin]);
            $formerTeacher = $request('GET', $teachingPath, cookie: '');
            self::assertSame(200, $formerTeacher->getStatusCode());
            self::assertStringNotContainsString('BEGIN:VEVENT', (string) $formerTeacher->getBody());
            self::assertSame(403, $request('POST', cookie: 'admin-session', body: '{"scope":"teaching"}')->getStatusCode());

            $missing = '/ical/unknown-token.ics';
            for ($attempt = 0; $attempt < 30; $attempt++) {
                self::assertSame(404, $request('GET', $missing, cookie: '')->getStatusCode());
            }
            $throttled = self::request($dsn, $pgPath, 'GET', $missing, '', '', ['X-Forwarded-For' => '192.0.2.9']);
            self::assertSame(429, $throttled->getStatusCode());
            self::assertSame('3600', $throttled->getHeaderLine('Retry-After'));
            self::assertSame(404, $request('GET', '/ical/another-token.ics', cookie: '')->getStatusCode());
            self::assertSame(404, self::request($dsn, $pgPath, 'GET', $missing, '', '', instant: '2026-12-04 13:00:00')->getStatusCode());
            $redacted = $request('GET', '/ical/my.secret.ics', cookie: '');
            self::assertSame(404, $redacted->getStatusCode());
            self::assertStringContainsString('/ical/<redacted>.ics', (string) $redacted->getBody());
            self::assertStringNotContainsString('my.secret', (string) $redacted->getBody());
            self::assertSame(422, $request('GET', $schedulePath . '?download=maybe', cookie: '')->getStatusCode());

            self::assertSame(400, $request('POST')->getStatusCode());
            self::assertSame(400, $request('POST', body: '{')->getStatusCode());
            self::assertSame(422, $request('POST', body: '[]')->getStatusCode());
            self::assertSame(422, $request('POST', body: '{"scope":"schedule","extra":true}')->getStatusCode());
            self::assertSame(422, $request('POST', body: '{"locationId":42}')->getStatusCode());
        } finally {
            if (!$postgres) {
                $db->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            foreach ($db->createSchemaManager()->listTableNames() as $table) {
                $db->executeStatement('DROP TABLE IF EXISTS ' . $db->quoteSingleIdentifier($table) . ($postgres ? ' CASCADE' : ''));
            }
            if ($postgres) {
                $db->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
            $db->close();
        }
    }

    /** @param array<string,string> $headers */
    private static function request(
        string $dsn,
        string $pgPath,
        string $method,
        string $path,
        string $cookie,
        string $body,
        array $headers = [],
        string $instant = self::NOW,
    ): ResponseInterface {
        $request = (new ServerRequestFactory())->createServerRequest($method, self::BASE . $path)->withHeader('Content-Type', 'application/json');
        if ($cookie !== '') {
            $request = $request->withCookieParams(['studio_session' => $cookie]);
        }
        foreach ($headers as $name => $value) {
            $request = $request->withHeader($name, $value);
        }
        $request->getBody()->write($body);
        $request->getBody()->rewind();
        $env = ['BASE_URL' => self::BASE, 'DATABASE_URL' => $dsn, 'LOG_LEVEL' => 'error'];
        $clock = new class ($instant) implements ClockInterface {
            public function __construct(private readonly string $instant) {}

            public function now(): DateTimeImmutable
            {
                return new DateTimeImmutable($this->instant . ' UTC');
            }
        };

        return Bootstrap::handle(static fn() => $request, dirname(__DIR__), '/missing', $pgPath, static fn(string $key): ?string => $env[$key] ?? null, clock: $clock);
    }

    /** @return ($status is 201 ? array{feed:array<string,mixed>,token:string,path:string,url?:string} : array{items:list<array<string,mixed>>}) */
    private static function json(ResponseInterface $response, int $status): array
    {
        self::assertSame($status, $response->getStatusCode(), (string) $response->getBody());
        $body = json_decode((string) $response->getBody(), true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($body);

        /** @var ($status is 201 ? array{feed:array<string,mixed>,token:string,path:string,url?:string} : array{items:list<array<string,mixed>>}) $body */
        return $body;
    }

    private static function string(mixed $value): string
    {
        self::assertIsString($value);
        return $value;
    }

    /** @param \Closure(string,string,string,string):ResponseInterface $request */
    private static function assertAttendanceAnnouncement(Connection $db, bool $postgres, \Closure $request): void
    {
        $member = self::id(self::MEMBER, $postgres);
        $admin = self::id(self::ADMIN, $postgres);
        $db->executeStatement('INSERT INTO person_guardians (guardian_id, dependent_id) VALUES (?, ?)', [$admin, $member]);
        $db->executeStatement("UPDATE bookings SET status = 'attended' WHERE person_id = ?", [$member]);
        try {
            $path = '/api/v1/admin/announcements';
            $visible = '/api/v1/announcements';
            $audience = '{"audience":"lesson","lessonId":"018f0000-0000-7000-8000-000000000012"';
            $preview = $request('POST', "$path/recipients", 'admin-session', $audience . '}');
            self::assertSame(200, $preview->getStatusCode());
            self::assertSame('{"recipients":1}', (string) $preview->getBody());
            $notice = $request('POST', $path, 'admin-session', $audience . ',"title":"Lesson notice","body":"Fixture note"}');
            self::assertSame(201, $notice->getStatusCode(), (string) $notice->getBody());
            $read = $request('GET', $visible, 'admin-session', '');
            self::assertSame(200, $read->getStatusCode());
            self::assertStringContainsString('Lesson notice', (string) $read->getBody());
            $memberRead = $request('GET', $visible, 'member-session', '');
            self::assertSame(200, $memberRead->getStatusCode());
            self::assertStringContainsString('Lesson notice', (string) $memberRead->getBody());
        } finally {
            $db->executeStatement("DELETE FROM announcements WHERE title = 'Lesson notice'");
            $db->executeStatement("UPDATE bookings SET status = 'booked' WHERE person_id = ?", [$member]);
            $db->executeStatement('DELETE FROM person_guardians WHERE guardian_id = ? AND dependent_id = ?', [$admin, $member]);
        }
    }

    private static function seedSchedule(Connection $db, bool $postgres, string $member, string $admin, string $location): void
    {
        $room = self::id('018f0000-0000-7000-8000-000000000010', $postgres);
        $type = self::id('018f0000-0000-7000-8000-000000000011', $postgres);
        $lesson = self::id('018f0000-0000-7000-8000-000000000012', $postgres);
        $booking = self::id('018f0000-0000-7000-8000-000000000013', $postgres);
        $durations = $postgres ? "'1 hour', '2 hours'" : '3600000000, 7200000000';
        $db->executeStatement(
            'INSERT INTO settings (school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time, '
            . 'release_weeks_ahead, default_visible_days, booking_cutoff, cancellation_cutoff) '
            . "VALUES ('Fictional Hall', 'Europe/Berlin', 'en', 'EUR', 'R', 0, '18:00:00', 1, 2, $durations)",
        );
        $db->executeStatement("INSERT INTO rooms (id, location_id, name, capacity, position) VALUES (?, ?, 'Studio A', 18, 1)", [$room, $location]);
        $db->executeStatement("INSERT INTO course_types (id, name) VALUES (?, 'Ballet')", [$type]);
        $db->executeStatement("INSERT INTO staff (id, person_id, display_name, kind, active) VALUES (?, ?, 'Ari Fable', 'instructor', FALSE)", [$admin, $admin]);
        $db->executeStatement(
            "INSERT INTO lessons (id, room_id, course_type_id, instructor_id, starts_at, ends_at, kind, visibility, capacity, status, cancel_note, canceled_at, canceled_by) "
            . "VALUES (?, ?, ?, ?, '2026-12-03 15:00:00', '2026-12-03 16:00:00', 'regular', 'hidden', 10, 'canceled', 'Hall unavailable', '2026-12-02 10:00:00', ?)",
            [$lesson, $room, $type, $admin, $admin],
        );
        $db->executeStatement(
            "INSERT INTO bookings (id, lesson_id, person_id, status, trial, booked_at) VALUES (?, ?, ?, 'booked', FALSE, '2026-12-01 10:00:00')",
            [$booking, $lesson, $member],
        );
    }

    private static function seedAccount(Connection $db, bool $postgres, string $id, string $role, string $sessionId): void
    {
        $db->executeStatement("INSERT INTO persons (id, given_name, family_name) VALUES (?, 'Ari', 'Fable')", [$id]);
        $db->executeStatement('INSERT INTO accounts (id, person_id, email, role, confirmed_at) VALUES (?, ?, ?, ?, ?)', [$id, $id, $role . '@example.test', $role, self::NOW]);
        self::seedSession($db, $postgres, $id, $role . '-session', $sessionId);
    }

    private static function seedSession(Connection $db, bool $postgres, string $account, string $cookie, string $id, ?string $impersonator = null): void
    {
        $digest = $postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $db->executeStatement(
            'INSERT INTO sessions (id, account_id, token_hash, created_at, last_seen_at, expires_at, impersonator_id) '
            . "VALUES (?, ?, $digest, ?, ?, ?, ?)",
            [self::id($id, $postgres), $account, hash('sha256', $cookie), self::NOW, self::NOW, '2026-12-05 12:00:00', $impersonator],
        );
    }

    private static function id(string $uuid, bool $postgres): string
    {
        return $postgres ? $uuid : pack('H*', str_replace('-', '', $uuid));
    }
}
