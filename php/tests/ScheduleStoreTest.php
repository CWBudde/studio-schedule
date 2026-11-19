<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Configuration;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\DriverManager;
use Doctrine\DBAL\Exception\DriverException;
use Doctrine\DBAL\Exception\ForeignKeyConstraintViolationException;
use Doctrine\DBAL\Logging\Middleware;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Log\AbstractLogger;
use Psr\Log\NullLogger;
use RuntimeException;
use Slim\App;
use Slim\Factory\AppFactory;
use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\Psr7\Factory\StreamFactory;
use Stringable;
use StudioSchedule\Http\CatalogAdminRoutes;
use StudioSchedule\Http\AttendanceRoutes;
use StudioSchedule\Http\EventRegistrationRoutes;
use StudioSchedule\Http\EventRoutes;
use StudioSchedule\Http\LessonAdminRoutes;
use StudioSchedule\Http\LessonNoticeRoutes;
use StudioSchedule\Http\LessonSignupRoutes;
use StudioSchedule\Http\PassIssueRoutes;
use StudioSchedule\Http\ReceiptReadRoutes;
use StudioSchedule\Http\ScheduleRoutes;
use StudioSchedule\Http\TeachingScheduleRoutes;
use StudioSchedule\Http\StatisticsEndpoint;
use StudioSchedule\Http\TrustedProxies;
use StudioSchedule\Http\WaitlistInviteEndpoint;
use StudioSchedule\Service\BackgroundJobs;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\Attendance;
use StudioSchedule\Mail\Renderer;
use StudioSchedule\Service\EventCancellations;
use StudioSchedule\Service\EventPayments;
use StudioSchedule\Service\LessonAdministration;
use StudioSchedule\Service\LessonRecipients;
use StudioSchedule\Service\LessonNotices;
use StudioSchedule\Service\LessonSignup;
use StudioSchedule\Service\LessonWaitlistOffers;
use StudioSchedule\Service\EventRegistrationReads;
use StudioSchedule\Service\EventRegistrationWrites;
use StudioSchedule\Service\EventWaitlist;
use StudioSchedule\Service\JobResult;
use StudioSchedule\Service\OneOffLesson;
use StudioSchedule\Service\PassIssuance;
use StudioSchedule\Service\PassVoiding;
use StudioSchedule\Service\PassRead;
use StudioSchedule\Service\ScheduleRecipients;
use StudioSchedule\Service\ReceiptIssuance;
use StudioSchedule\Service\ReceiptCancellation;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\StatisticsService;
use StudioSchedule\Service\TeachingSchedule;
use StudioSchedule\Service\WaitlistInvites;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\AttendanceStore;
use StudioSchedule\Store\EventRegistrationStore;
use StudioSchedule\Store\ReceiptWriteStore;
use StudioSchedule\Store\ReceiptReadStore;
use StudioSchedule\Store\EventStore;
use StudioSchedule\Store\EventWaitlistStore;
use StudioSchedule\Store\FamilyStore;
use StudioSchedule\Store\LessonWriteStore;
use StudioSchedule\Store\LessonNoticeStore;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\ParticipantReadStore;
use StudioSchedule\Store\PassIssueStore;
use StudioSchedule\Store\PassLedgerStore;
use StudioSchedule\Store\PassReadStore;
use StudioSchedule\Store\PublicSettingsStore;
use StudioSchedule\Store\RateLimitStore;
use StudioSchedule\Store\ScheduleStore;
use StudioSchedule\Store\Schema;
use StudioSchedule\Store\StatisticsStore;
use StudioSchedule\Store\TimetableStore;
use StudioSchedule\Store\WaitlistInviteStore;

/** @phpstan-import-type ReceiptDocument from \StudioSchedule\Receipt\Renderer */
final class ScheduleStoreTest extends TestCase
{
    private const string RANGE = '/schedule?from=2026-10-25&to=2026-11-05';
    private const string BEFORE_RELEASE = '2026-10-25T16:59:59Z';
    private const string AT_RELEASE = '2026-10-25T17:00:00Z';

    private Connection $database;
    private bool $postgres;
    private bool $fixtureDatabase = false;

    protected function setUp(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database provisioned by just php-test');
        }

        $this->database = Database::connect($dsn);
        $this->postgres = $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        self::assertSame([], (new Schema($this->database))->relations(), 'Schedule tests require an empty disposable database');
        $this->fixtureDatabase = true;
        $migrations = $this->postgres
            ? (getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: __DIR__ . '/../../internal/store/postgres/migrations')
            : __DIR__ . '/../migrations/mysql';

        (new Migrator($dsn))->up($migrations);
        $this->seedSchedule();
        if ($this->postgres) {
            $this->database->executeStatement("SET DateStyle TO 'SQL, DMY'");
        }
    }

    protected function tearDown(): void
    {
        if (!$this->fixtureDatabase) {
            return;
        }

        try {
            if (!$this->postgres) {
                $this->database->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            foreach ($this->database->createSchemaManager()->listTableNames() as $table) {
                $suffix = $this->postgres ? ' CASCADE' : '';
                $this->database->executeStatement('DROP TABLE IF EXISTS ' . $this->database->quoteSingleIdentifier($table) . $suffix);
            }
            if ($this->postgres) {
                $this->database->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
        } finally {
            $this->database->close();
        }
    }

    public function testOfficeCreatesOneOffLessonsWithSchoolTimeAndValidatesReferencesAndConflicts(): void
    {
        $app = $this->app('2026-10-24T12:00:00Z');
        $office = new SessionPrincipal(self::id(11), self::id(1), 'office');
        $data = [
            'roomId' => self::id(21), 'courseTypeId' => self::id(22), 'instructorId' => self::id(23),
            'accompanistId' => self::id(24), 'date' => '2026-10-29', 'startTime' => '11:30',
            'durationMinutes' => 60, 'visibility' => 'members', 'kind' => 'private', 'capacity' => 3,
        ];
        $post = static function (array $fields, int $status) use ($app, $office): array {
            $request = (new ServerRequestFactory())->createServerRequest('POST', '/api/v1/lessons')
                ->withAttribute('sessionPrincipal', $office)
                ->withHeader('Content-Type', 'application/json')
                ->withBody((new StreamFactory())->createStream(json_encode($fields, JSON_THROW_ON_ERROR)));
            $response = $app->handle($request);
            self::assertSame($status, $response->getStatusCode(), (string) $response->getBody());
            self::assertSame('no-store', $response->getHeaderLine('Cache-Control'));
            $body = json_decode((string) $response->getBody(), true, flags: JSON_THROW_ON_ERROR);
            self::assertIsArray($body);

            return $body;
        };

        $unknown = self::id(255);
        $invalid = [
            ['date', '2026-02-30'], ['startTime', '29:00'], ['durationMinutes', 1000], ['capacity', 1001],
            ['courseTypeId', $unknown], ['courseTypeId', null], ['instructorId', $unknown], ['instructorId', null],
            ['roomId', $unknown], ['accompanistId', $unknown], ['accompanistId', self::id(23)],
            ['capacity', 0], ['visibility', 'other'], ['kind', 'other'], ['renter', 'A renter'],
            ['confirmPast', 'yes'], ['unexpected', true],
        ];
        foreach ($invalid as [$field, $value]) {
            $body = $post(array_replace($data, [$field => $value]), 422);
            self::assertStringContainsString('"location":"body.' . $field . '"', json_encode($body, JSON_THROW_ON_ERROR));
        }

        $past = array_replace($data, ['date' => '2026-10-24', 'startTime' => '14:00']);
        self::assertSame('past', $post($past, 409)['reason']);
        $post($past + ['confirmPast' => true], 201);

        // The only prior lesson in this room at this time is canceled.
        $created = $post($data, 201);
        self::assertSame('2026-10-29T10:30:00Z', $created['startsAt']);
        self::assertSame('2026-10-29T11:30:00Z', $created['endsAt']);
        self::assertSame(3, $created['freePlaces']);
        self::assertSame(['activeBookings' => 0, 'detached' => false], $created['admin']);
        self::assertArrayNotHasKey('classId', $created);
        $id = $created['id'];
        self::assertIsString($id);
        self::assertEquals($created, self::get($app, '/lessons/' . $id, $office));
        $conflict = $post($data, 409);
        self::assertSame('room-taken', $conflict['reason']);
        self::assertSame([
            'id' => $id, 'startsAt' => $created['startsAt'], 'endsAt' => $created['endsAt'], 'courseType' => $created['courseType'],
        ], $conflict['lesson']);
        $post($data + ['confirmRoomOverlap' => true], 201);
        $post(array_replace($data, ['startTime' => '12:30']), 201);

        $rent = array_replace($data, [
            'date' => '2026-10-25', 'startTime' => '02:30', 'kind' => 'rental', 'visibility' => 'hidden', 'capacity' => 0,
            'courseTypeId' => null, 'instructorId' => null, 'accompanistId' => null, 'renter' => ' Fixture club ',
        ]);
        foreach ([
            ['instructorId', self::id(23)], ['accompanistId', self::id(24)], ['capacity', 1],
            ['visibility', 'public'], ['renter', ' '],
        ] as [$field, $value]) {
            $body = $post(array_replace($rent, [$field => $value]), 422);
            self::assertStringContainsString('"location":"body.' . $field . '"', json_encode($body, JSON_THROW_ON_ERROR));
        }

        $rental = $post($rent, 201);
        self::assertSame('2026-10-25T00:30:00Z', $rental['startsAt']);
        self::assertSame('2026-10-25T01:30:00Z', $rental['endsAt']);
        self::assertSame('Fixture club', $rental['renter']);
        self::assertSame(0, $rental['freePlaces']);
        self::assertArrayNotHasKey('courseType', $rental);
        self::assertArrayNotHasKey('instructor', $rental);
        self::assertSame([
            'id' => $rental['id'], 'startsAt' => $rental['startsAt'], 'endsAt' => $rental['endsAt'],
        ], $post($rent, 409)['lesson']);

        $this->database->executeStatement("UPDATE settings SET timezone = 'America/New_York'");
        foreach ([20 => 'date', 18 => 'durationMinutes'] as $hour => $field) {
            $body = $post(array_replace($data, [
                'date' => '9999-12-31', 'startTime' => $hour . ':30', 'durationMinutes' => 90,
            ]), 422);
            self::assertStringContainsString('"location":"body.' . $field . '"', json_encode($body, JSON_THROW_ON_ERROR));
        }
        $this->database->executeStatement("UPDATE settings SET timezone = 'UTC'");
        $body = $post(array_replace($data, ['date' => '0001-01-01', 'startTime' => '00:00', 'confirmPast' => true]), 422);
        self::assertStringContainsString('"location":"body.date"', json_encode($body, JSON_THROW_ON_ERROR));

        if ($this->postgres) {
            $this->database->executeStatement('SET SESSION CHARACTERISTICS AS TRANSACTION ISOLATION LEVEL REPEATABLE READ');
            $isolation = fn(): mixed => $this->database->fetchOne('SHOW transaction_isolation');
            self::assertSame('repeatable read', $isolation());
            self::assertSame('read committed', (new TimetableStore($this->database))->writeOneOff($isolation));
            (new PublicSettingsStore($this->database))->changeRegional(static function (array $current) use ($isolation): array {
                self::assertSame('read committed', $isolation());

                return $current;
            });
        }
    }

    public function testWaitlistInvitePreviewShowsInviteStateWithoutTakingAPlace(): void
    {
        $this->insert('waitlist_entries', [
            'id' => 215, 'lesson_id' => 100, 'person_id' => 5, 'status' => 'waiting',
            'stay_on_list' => true, 'joined_at' => '2026-10-22 14:00:00',
            'invite_token_hash' => hash('sha256', 'requeued-test-token', true),
        ]);
        $this->insert('waitlist_entries', [
            'id' => 216, 'lesson_id' => 103, 'person_id' => 5, 'status' => 'withdrawn',
            'stay_on_list' => false, 'joined_at' => '2026-10-22 14:00:00', 'invited_at' => '2026-10-23 14:00:00',
            'expires_at' => '2026-10-27 14:00:00', 'invite_token_hash' => hash('sha256', 'withdrawn-test-token', true),
        ]);

        $this->insert('waitlist_entries', [
            'id' => 217, 'lesson_id' => 107, 'person_id' => 5, 'status' => 'withdrawn',
            'stay_on_list' => false, 'joined_at' => '2026-10-22 14:00:00', 'invited_at' => '2026-10-23 14:00:00',
            'expires_at' => '2026-10-27 14:00:00', 'invite_token_hash' => hash('sha256', 'historical-session-token', true),
        ]);

        $app = $this->app('2026-10-25T09:00:00Z');
        $path = '/waitlist-invites/';
        $active = self::get($app, $path . '213');
        self::assertSame('invited', $active['state']);
        self::assertSame('Third', $active['givenName']);
        self::assertSame('2026-10-26T17:00:00Z', $active['expiresAt']);
        self::assertArrayNotHasKey('closed', $active);
        self::assertArrayNotHasKey('position', $active);
        self::assertSame(self::id(100), self::inviteLessonId($active));
        self::assertArrayNotHasKey('booking', $active);

        $requeued = self::get($app, $path . 'requeued-test-token');
        self::assertSame('waiting', $requeued['state']);
        self::assertSame(3, $requeued['position']);
        self::assertArrayNotHasKey('expiresAt', $requeued);
        $withdrawn = self::get($app, $path . 'withdrawn-test-token');
        self::assertSame('withdrawn', $withdrawn['state']);
        self::assertSame('canceled', $withdrawn['closed']);
        self::assertSame('2026-10-27T14:00:00Z', $withdrawn['expiresAt']);
        self::assertSame(self::id(103), self::inviteLessonId($withdrawn));
        $historicalSession = self::get($app, $path . 'historical-session-token')['lesson'];
        self::assertIsArray($historicalSession);
        self::assertSame(self::id(121), $historicalSession['eventId']);
        self::assertSame(4, $historicalSession['capacity']);
        self::assertArrayNotHasKey('eventSlug', $historicalSession);

        $atCutoff = self::get($this->app('2026-10-25T09:59:59.999999Z'), $path . '213');
        self::assertSame('cutoff', $atCutoff['closed']);
        $expired = self::get($this->app('2026-10-25T16:59:59Z'), $path . '214');
        self::assertSame('expired', $expired['state']);
        self::assertSame('started', $expired['closed']);
        self::assertSame('2026-10-25T16:59:59Z', $expired['expiresAt']);

        $request = (new ServerRequestFactory())->createServerRequest('GET', '/api/v1' . $path . '213');
        $response = $app->handle($request);
        self::assertSame(200, $response->getStatusCode());
        self::assertSame('no-store', $response->getHeaderLine('Cache-Control'));
        self::assertSame(405, $app->handle($request->withMethod('HEAD'))->getStatusCode());
        self::get($app, $path . str_repeat('a', 129), status: 422);
        self::get($app, $path . 'unrecognized-token', status: 404);
        self::assertContains($this->database->fetchOne('SELECT COUNT(*) FROM bookings'), [4, '4']);
        self::assertContains($this->database->fetchOne("SELECT COUNT(*) FROM waitlist_entries WHERE status = 'invited'"), [2, '2']);
    }

    /** @param array<array-key,mixed> $invite */
    private static function inviteLessonId(array $invite): string
    {
        $lesson = $invite['lesson'];
        self::assertIsArray($lesson);
        $id = $lesson['id'];
        self::assertIsString($id);

        return $id;
    }

    public function testPublicEventsIncludeOnlyUpcomingPublishedEventsAndUseTheirOwnSignupWindows(): void
    {
        $this->insertLesson(108, '2026-10-27', 'workshop', 'public', [
            'event_id' => 120, 'status' => 'canceled', 'canceled_at' => '2026-10-25 13:00:00',
        ]);
        $create = [
            'course_type_id' => 22, 'capacity' => 4, 'price' => 2500, 'vat_rate_bp' => 1900,
            'created_at' => '2026-10-24 10:00:00', 'updated_at' => '2026-10-24 10:00:00',
        ];
        $this->insert('events', [
            'id' => 122, 'slug' => 'evening-workshop', 'title' => 'Evening workshop', 'description' => 'A fictional event',
            'status' => 'published', 'signup_opens_at' => '2026-10-25 17:00:00',
            'signup_closes_at' => '2026-10-26 17:00:00', 'cancel_until' => '2026-10-26 16:00:00',
        ] + $create);
        $this->insertLesson(109, '2026-10-30', 'workshop', 'public', ['event_id' => 122]);
        $this->insert('event_images', [
            'event_id' => 122, 'image' => 'synthetic image', 'image_type' => 'image/png',
            'image_digest' => str_repeat('0', 64),
        ]);
        $this->insert('events', [
            'id' => 123, 'slug' => 'canceled-workshop', 'title' => 'Canceled workshop', 'status' => 'canceled',
        ] + $create);
        $this->insertLesson(110, '2026-11-02', 'workshop', 'public', ['event_id' => 123]);
        $this->insert('events', [
            'id' => 124, 'slug' => 'canceled-session', 'title' => 'Canceled session', 'status' => 'published',
        ] + $create);
        $this->insertLesson(111, '2026-11-02', 'workshop', 'public', [
            'event_id' => 124, 'status' => 'canceled', 'canceled_at' => '2026-10-25 13:00:00',
        ]);

        $before = self::get($this->app(self::BEFORE_RELEASE), '/events');
        $items = $before['items'];
        self::assertIsArray($items);
        self::assertSame(['winter-workshop', 'evening-workshop'], array_column($items, 'slug'));
        [$winter, $evening] = $items;
        self::assertIsArray($winter);
        self::assertIsArray($evening);
        self::assertSame(7, $winter['capacity']);
        self::assertSame(5, $winter['freePlaces']);
        self::assertSame('2026-11-03T10:00:00.5Z', $winter['signupClosesAt']);
        self::assertSame($winter['signupClosesAt'], $winter['cancelUntil']);
        self::assertTrue($winter['signupOpen']);
        self::assertFalse($winter['hasImage']);
        self::assertArrayNotHasKey('signupOpensAt', $winter);
        self::assertArrayNotHasKey('description', $winter);
        self::assertArrayNotHasKey('registered', $winter);
        self::assertIsArray($winter['sessions']);
        self::assertSame(self::ids(108, 106), array_column($winter['sessions'], 'id'));
        self::assertSame(['canceled', 'scheduled'], array_column($winter['sessions'], 'status'));
        self::assertSame([4, 4], array_column($winter['sessions'], 'capacity'));
        self::assertSame(['winter-workshop', 'winter-workshop'], array_column($winter['sessions'], 'eventSlug'));
        self::assertSame('A fictional event', $evening['description']);
        self::assertSame(2500, $evening['price']);
        self::assertSame(1900, $evening['vatRate']);
        self::assertFalse($evening['signupOpen']);
        self::assertTrue($evening['hasImage']);
        self::assertSame('2026-10-25T17:00:00Z', $evening['signupOpensAt']);
        self::assertSame('2026-10-26T17:00:00Z', $evening['signupClosesAt']);
        self::assertSame('2026-10-26T16:00:00Z', $evening['cancelUntil']);

        $whenOpen = self::get($this->app(self::AT_RELEASE), '/events');
        self::assertIsArray($whenOpen['items']);
        self::assertSame([true, true], array_column($whenOpen['items'], 'signupOpen'));
        self::assertSame(['items' => []], self::get($this->app('2026-11-03T11:00:00.5Z'), '/events'));
    }

    public function testPublicEventDetailsAndImagesIncludeCanceledEventsAndHideDrafts(): void
    {
        $this->insert('events', [
            'id' => 122, 'slug' => 'canceled-workshop', 'title' => 'Canceled workshop', 'status' => 'canceled',
            'course_type_id' => 22, 'capacity' => 6, 'price' => 2500, 'vat_rate_bp' => 1900,
            'created_at' => '2026-10-24 10:00:00', 'updated_at' => '2026-10-25 12:00:00',
            'signup_closes_at' => '2026-11-02 08:00:00',
        ]);
        $this->insertLesson(110, '2026-11-02', 'workshop', 'public', [
            'event_id' => 122, 'status' => 'canceled', 'canceled_at' => '2026-10-25 12:00:00',
        ]);
        $png = base64_decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aWZkAAAAASUVORK5CYII=');
        $digest = hash('sha256', "image/png\n" . $png);
        foreach ([121, 122] as $eventId) {
            $this->insert('event_images', [
                'event_id' => $eventId, 'image' => $png, 'image_type' => 'image/png', 'image_digest' => $digest,
            ]);
        }

        $app = $this->app(self::BEFORE_RELEASE);
        $published = self::get($app, '/events/winter-workshop');
        self::assertSame('published', $published['status']);
        self::assertSame(5, $published['freePlaces']);
        self::assertTrue($published['signupOpen']);
        $past = self::get($this->app('2026-11-04T12:00:00Z'), '/events/winter-workshop');
        self::assertFalse($past['signupOpen']);
        self::assertSame(6, $past['freePlaces']);
        $canceled = self::get($app, '/events/canceled-workshop');
        self::assertSame('canceled', $canceled['status']);
        self::assertFalse($canceled['signupOpen']);
        self::assertTrue($canceled['hasImage']);
        self::assertIsArray($canceled['sessions']);
        self::assertSame(['canceled'], array_column($canceled['sessions'], 'status'));
        foreach (['draft-workshop', 'Winter-workshop', 'missing-workshop'] as $slug) {
            self::assertSame('resource not found', self::get($app, '/events/' . $slug, status: 404)['detail']);
            self::assertSame('resource not found', self::get($app, '/events/' . $slug . '/image', status: 404)['detail']);
        }
        self::assertSame('no image', self::get($app, '/events/winter-workshop/image', status: 404)['detail']);
        self::assertSame('resource not found', self::get($app, '/events/canceled-workshop%2Fimage', status: 404)['detail']);
        foreach (['', '/image'] as $suffix) {
            $invalid = self::get($app, '/events/' . str_repeat('a', 81) . $suffix, status: 422);
            self::assertIsArray($invalid['errors']);
            self::assertSame(['path.slug'], array_column($invalid['errors'], 'location'));
        }

        $image = static function (string $ifNoneMatch) use ($app): ResponseInterface {
            $request = (new ServerRequestFactory())->createServerRequest('GET', '/api/v1/events/canceled-workshop/image')
                ->withHeader('If-None-Match', $ifNoneMatch);

            return $app->handle($request);
        };
        $etag = '"' . $digest . '"';
        $response = $image('"other"');
        self::assertSame(200, $response->getStatusCode());
        self::assertSame($png, (string) $response->getBody());
        self::assertSame('image/png', $response->getHeaderLine('Content-Type'));
        self::assertSame('inline', $response->getHeaderLine('Content-Disposition'));
        self::assertSame('public, no-cache', $response->getHeaderLine('Cache-Control'));
        self::assertSame($etag, $response->getHeaderLine('ETag'));
        foreach ([$etag, '"other", W/' . $etag, '*'] as $validator) {
            $unchanged = $image($validator);
            self::assertSame(304, $unchanged->getStatusCode());
            self::assertSame('', (string) $unchanged->getBody());
            self::assertSame($etag, $unchanged->getHeaderLine('ETag'));
        }
    }

    public function testOfficeEventReadsIncludePrivateAndHistoricalEventsAndTheirImages(): void
    {
        $fields = [
            'course_type_id' => 22, 'capacity' => 7, 'price' => 2500, 'vat_rate_bp' => 1900,
            'created_at' => '2026-10-24 10:00:00', 'updated_at' => '2026-10-25 12:00:00.125',
        ];
        $this->insert('events', [
            'id' => 118, 'slug' => 'without-session', 'title' => 'Without a session', 'status' => 'draft',
        ] + $fields);
        $this->insert('events', [
            'id' => 119, 'slug' => 'canceled-workshop', 'title' => 'Canceled workshop', 'status' => 'canceled',
            'signup_closes_at' => '2026-11-02 08:00:00',
        ] + $fields);
        $this->insertLesson(108, '2026-11-02', 'workshop', 'public', [
            'event_id' => 119, 'status' => 'canceled', 'canceled_at' => '2026-10-25 12:00:00',
        ]);
        $this->insert('event_registrations', [
            'id' => 224, 'event_id' => 120, 'person_id' => 4, 'status' => 'waiting', 'payment' => 'unpaid',
            'price' => 0, 'vat_rate_bp' => 0, 'created_at' => '2026-10-22 14:00:00', 'updated_at' => '2026-10-23 14:00:00',
        ]);
        $png = base64_decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aWZkAAAAASUVORK5CYII=');
        $digest = hash('sha256', "image/png\n" . $png);
        $this->insert('event_images', [
            'event_id' => 121, 'image' => $png, 'image_type' => 'image/png', 'image_digest' => $digest,
        ]);

        $app = $this->app(self::BEFORE_RELEASE);
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office', null, null);
        $events = '/admin/events';
        $draftPath = $events . '/' . self::id(121);
        $list = self::get($app, $events, $office)['items'];
        self::assertIsArray($list);
        self::assertSame(self::ids(119, 120, 121, 118), array_column($list, 'id'));
        [$canceled, $published, $draft, $withoutSession] = $list;
        self::assertSame([], array_column($list, 'image'));
        self::assertIsArray($canceled);
        self::assertIsArray($published);
        self::assertIsArray($draft);
        self::assertIsArray($withoutSession);
        self::assertSame('canceled', $canceled['status']);
        self::assertSame(self::id(22), $canceled['courseTypeId']);
        self::assertSame('2026-10-25T12:00:00.125Z', $canceled['updatedAt']);
        self::assertSame('2026-11-02T08:00:00Z', $canceled['signupClosesAt']);
        self::assertIsArray($canceled['sessions']);
        self::assertSame(['canceled'], array_column($canceled['sessions'], 'status'));
        self::assertSame('draft', $draft['status']);
        self::assertSame([], $withoutSession['sessions']);
        self::assertSame([1, 1, 1], [$published['registered'], $published['invited'], $published['waiting']]);
        self::assertArrayNotHasKey('signupClosesAt', $published);
        $past = self::get($this->app('2026-10-26T17:00:00Z'), $events . '/' . self::id(120), $office);
        self::assertSame(0, $past['invited']);

        $detail = self::get($app, $draftPath, $office);
        self::assertSame($draft + ['image' => ['contentType' => 'image/png', 'digest' => $digest]], $detail);
        self::get($app, $events . '/invalid', $office, status: 422);
        self::get($app, $events . '/' . self::id(999), $office, status: 404);
        self::get($app, $events . '/' . self::id(120) . '/image', $office, status: 404);
        self::get($app, $draftPath . '/image', status: 401);

        $request = (new ServerRequestFactory())->createServerRequest('GET', '/api/v1' . $draftPath . '/image')
            ->withAttribute('sessionPrincipal', $office);
        $image = $app->handle($request);
        self::assertSame(200, $image->getStatusCode());
        self::assertSame('image/png', $image->getHeaderLine('Content-Type'));
        self::assertSame('"' . $digest . '"', $image->getHeaderLine('ETag'));
        self::assertSame($png, (string) $image->getBody());
        $unchanged = $app->handle($request->withHeader('If-None-Match', 'W/"' . $digest . '"'));
        self::assertSame(304, $unchanged->getStatusCode());
        self::assertSame('', (string) $unchanged->getBody());
    }

    public function testOfficeEventLifecycleCommitsTheActorAndProtectsStartedSessions(): void
    {
        $app = $this->app(self::AT_RELEASE);
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office', null, self::id(10));
        /** @param array<string,mixed>|null $body */
        $call = static function (string $method, string $path, int $status, ?array $body = null) use ($app, $office): ResponseInterface {
            $request = (new ServerRequestFactory())->createServerRequest($method, '/api/v1' . $path)
                ->withAttribute('sessionPrincipal', $office);
            if ($body !== null) {
                $request = $request->withHeader('Content-Type', 'application/json')
                    ->withBody((new StreamFactory())->createStream(json_encode($body, JSON_THROW_ON_ERROR)));
            }
            $result = $app->handle($request);
            self::assertSame($status, $result->getStatusCode(), (string) $result->getBody());

            return $result;
        };
        $base = '/admin/events';
        $input = [
            'title' => ' À la carte ', 'courseTypeId' => self::id(22), 'capacity' => 1700, 'price' => 2500, 'vatRate' => 1900,
            'description' => ' A draft event ', 'signupOpensAt' => '2026-10-26T11:00:00+01:00',
            'signupClosesAt' => '2026-11-04T12:00:00+01:00', 'cancelUntil' => '2026-11-04T10:00:00Z',
        ];
        /** @param array<string,mixed> $fields */
        $wrong = static fn(array $fields): ResponseInterface => $call('POST', $base, 422, array_replace($input, $fields));
        self::assertStringContainsString('body.slug', (string) $wrong(['title' => '  '])->getBody());
        self::assertStringContainsString('body.title', (string) $wrong(['title' => '  ', 'slug' => 'blank-title'])->getBody());
        self::assertStringContainsString('body.slug', (string) $wrong(['slug' => 'bad--slug'])->getBody());
        self::assertStringContainsString('body.slug', (string) $wrong(['title' => '♪'])->getBody());
        self::assertStringContainsString('body.signupOpensAt', (string) $wrong(['signupOpensAt' => '2026-11-31T12:00:00Z'])->getBody());
        self::assertStringContainsString('body.signupClosesAt', (string) $wrong(['signupClosesAt' => '2026-10-26T10:00:00Z'])->getBody());
        self::assertStringContainsString('body.signupClosesAt', (string) $wrong(['signupClosesAt' => '9999-12-31T23:59:59-01:00'])->getBody());
        self::assertStringContainsString('body.courseTypeId', (string) $wrong(['courseTypeId' => self::id(999)])->getBody());

        $created = $call('POST', $base, 201, $input);
        $response = json_decode((string) $created->getBody(), true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($response);
        self::assertIsString($response['id']);
        self::assertSame('a-la-carte', $response['slug']);
        self::assertSame('À la carte', $response['title']);
        self::assertSame('A draft event', $response['description']);
        self::assertSame('2026-10-26T10:00:00Z', $response['signupOpensAt']);
        self::assertSame('2026-11-04T11:00:00Z', $response['signupClosesAt']);
        self::assertSame('2026-11-04T10:00:00Z', $response['cancelUntil']);
        self::assertSame(self::AT_RELEASE, $response['createdAt']);
        $actor = self::id(10);
        self::assertSame($this->postgres ? $actor : pack('H*', str_replace('-', '', $actor)), $this->database->fetchOne(
            'SELECT created_by FROM events WHERE slug = ?',
            ['a-la-carte'],
        ));
        $numbered = $call('POST', $base, 201, $input);
        self::assertStringContainsString('"slug":"a-la-carte-2"', (string) $numbered->getBody());
        $duplicate = $call('POST', $base, 409, $input + ['slug' => 'a-la-carte']);
        self::assertStringContainsString('"reason":"duplicate"', (string) $duplicate->getBody());

        $path = $base . '/' . $response['id'];
        $publish = $path . '/publish';
        $unpublish = $path . '/unpublish';
        self::assertStringContainsString('"reason":"no-sessions"', (string) $call('POST', $publish, 409)->getBody());
        $data = [
            'date' => '2026-11-05', 'startTime' => '10:00', 'durationMinutes' => 60,
            'roomId' => self::id(21), 'instructorId' => self::id(23),
        ];
        $session = $call('POST', $path . '/sessions', 201, $data);
        self::assertStringContainsString('"eventSlug":"a-la-carte"', (string) $session->getBody());
        self::assertStringContainsString('"capacity":1700', (string) $session->getBody());
        $overlap = $call('POST', $path . '/sessions', 409, $data);
        self::assertStringContainsString('"reason":"room-taken"', (string) $overlap->getBody());
        self::assertStringContainsString('"status":"published"', (string) $call('POST', $publish, 200)->getBody());
        self::assertStringContainsString('"reason":"not-draft"', (string) $call('POST', $publish, 409)->getBody());
        self::assertStringContainsString('"status":"draft"', (string) $call('POST', $unpublish, 200)->getBody());
        self::assertStringContainsString('"reason":"not-published"', (string) $call('POST', $unpublish, 409)->getBody());

        $image = $path . '/image';
        $call('PUT', $image, 422, ['contentType' => 'image/jpeg', 'data' => 'aGVsbG8=']);
        $call('PUT', $image, 204, ['contentType' => 'image/png', 'data' => 'iVBORw0KGgo=']);
        $call('PUT', $image, 204, ['contentType' => 'image/jpeg', 'data' => '/9j/']);
        $asset = $call('GET', $image, 200);
        self::assertSame('image/jpeg', $asset->getHeaderLine('Content-Type'));
        self::assertSame("\xff\xd8\xff", (string) $asset->getBody());
        self::assertSame('"' . hash('sha256', "image/jpeg\n\xff\xd8\xff") . '"', $asset->getHeaderLine('ETag'));
        $call('DELETE', $image, 204);
        $call('GET', $image, 404);
        $call('PUT', $image, 204, ['contentType' => 'image/png', 'data' => 'iVBORw0KGgo=']);
        $call('DELETE', $path, 204);
        $call('DELETE', $path, 404);
        self::assertContains($this->database->fetchOne('SELECT COUNT(*) FROM event_images'), [0, '0']);

        $past = $base . '/' . self::id(121);
        $data['date'] = '2026-10-24';
        self::assertStringContainsString('"reason":"past"', (string) $call('POST', $past . '/sessions', 409, $data)->getBody());
        $call('POST', $past . '/sessions', 201, $data + ['confirmPast' => true, 'confirmRoomOverlap' => true]);
        self::assertStringContainsString('"reason":"in-use"', (string) $call('DELETE', $past, 409)->getBody());
        $registered = $base . '/' . self::id(120);
        self::assertStringContainsString('"reason":"in-use"', (string) $call('DELETE', $registered, 409)->getBody());
        self::assertStringContainsString('"reason":"in-use"', (string) $call('POST', $registered . '/unpublish', 409)->getBody());

        foreach ([122 => 'draft', 123 => 'canceled'] as $id => $status) {
            $this->insert('events', [
                'id' => $id, 'slug' => 'test-event-' . $id, 'title' => 'Test event', 'status' => $status,
                'course_type_id' => 22, 'capacity' => 5, 'price' => 0, 'vat_rate_bp' => 0,
                'created_at' => '2026-10-24 10:00:00', 'updated_at' => '2026-10-24 10:00:00',
            ]);
        }
        $data['date'] = '2026-11-05';
        $closed = $call('POST', $base . '/' . self::id(123) . '/sessions', 409, $data);
        self::assertStringContainsString('"reason":"closed"', (string) $closed->getBody());
        $draft = $base . '/' . self::id(122);
        $this->insertLesson(108, '2026-11-07', 'workshop', 'public', [
            'event_id' => 122, 'status' => 'canceled', 'canceled_at' => '2026-10-25 14:00:00',
        ]);
        self::assertStringContainsString('"reason":"no-sessions"', (string) $call('POST', $draft . '/publish', 409)->getBody());

        // A legacy booking on an event session restricts deletion of that lesson.
        $this->insertLesson(109, '2026-11-08', 'workshop', 'public', ['event_id' => 122]);
        $this->insert('bookings', [
            'id' => 250, 'lesson_id' => 109, 'person_id' => 1, 'status' => 'booked', 'trial' => false, 'booked_at' => '2026-10-24 14:00:00',
        ]);
        $failed = $call('DELETE', $draft, 409);
        self::assertStringContainsString('"detail":"conflict"', (string) $failed->getBody());
        $preserved = self::get($app, $draft, $office);
        self::assertIsArray($preserved['sessions']);
        self::assertSame(self::ids(108, 109), array_column($preserved['sessions'], 'id'));

        $this->insertLesson(110, '2026-10-24', 'workshop', 'public', ['event_id' => 122]);
        $edit = ['title' => 'Updated event', 'slug' => 'edited-workshop', 'capacity' => 6, 'price' => 12000, 'vatRate' => 1900,
            'courseTypeId' => self::id(22)];
        $call('PUT', $draft, 400);
        $call('PUT', $base . '/' . self::id(999), 404, $edit);
        $conflict = $call('PUT', $draft, 409, array_replace($edit, ['slug' => 'winter-workshop']));
        self::assertStringContainsString('"reason":"duplicate"', (string) $conflict->getBody());
        $invalid = $call('PUT', $draft, 422, array_replace($edit, ['courseTypeId' => self::id(999)]));
        self::assertStringContainsString('body.courseTypeId', (string) $invalid->getBody());
        $resized = json_decode((string) $call('PUT', $draft, 200, $edit)->getBody(), true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($resized);
        self::assertSame('edited-workshop', $resized['slug']);
        self::assertSame('draft', $resized['status']);
        self::assertSame(12000, $resized['price']);
        self::assertIsArray($resized['sessions']);
        self::assertSame(self::ids(110, 108, 109), array_column($resized['sessions'], 'id'));
        self::assertSame([4, 6, 6], array_column($resized['sessions'], 'capacity'));
        self::assertSame(['scheduled', 'canceled', 'scheduled'], array_column($resized['sessions'], 'status'));
    }

    public function testEventRegistrationReadsRespectFamilyPrivacyAndInviteCutoffs(): void
    {
        $this->insert('events', [
            'id' => 122, 'slug' => 'sessionless', 'title' => 'Sessionless event', 'course_type_id' => 22,
            'status' => 'published', 'capacity' => 5, 'price' => 0, 'vat_rate_bp' => 0,
            'created_at' => '2026-10-20 10:00:00', 'updated_at' => '2026-10-24 12:00:00',
        ]);
        $free = ['payment' => 'paid', 'price' => 0, 'vat_rate_bp' => 1900];
        $timestamps = ['created_at' => '2026-10-22 14:00:00', 'updated_at' => '2026-10-23 14:00:00'];
        $this->insert('event_registrations', [
            'id' => 224, 'event_id' => 120, 'person_id' => 1, 'status' => 'canceled',
            'created_at' => '2026-10-20 14:00:00.25', 'updated_at' => '2026-10-21 14:00:00', 'canceled_at' => '2026-10-21 14:00:00',
        ] + $free);
        $this->insert('event_registrations', [
            'id' => 225, 'event_id' => 121, 'person_id' => 1, 'status' => 'registered',
        ] + $free + $timestamps);
        $this->insert('event_registrations', [
            'id' => 226, 'event_id' => 120, 'guest_name' => 'Guest', 'status' => 'canceled', 'canceled_at' => '2026-10-23 14:00:00',
        ] + $free + $timestamps);
        $this->insert('event_registrations', [
            'id' => 227, 'event_id' => 122, 'person_id' => 6, 'status' => 'registered',
        ] + $free + $timestamps);
        $this->database->executeStatement("UPDATE persons SET anonymized_at = '2026-10-24 12:00:00' WHERE given_name = 'Zoe'");

        $member = new SessionPrincipal(self::id(10), self::id(1), 'member', null, null);
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office', null, null);
        $app = $this->app(self::BEFORE_RELEASE);
        $rosterPath = '/admin/events/' . self::id(120) . '/registrations';
        self::get($app, $rosterPath, status: 401);
        self::get($app, $rosterPath, $member, 403);
        self::get($app, '/admin/events/invalid/registrations', $office, 422);
        $roster = self::get($app, $rosterPath, $office)['items'];
        self::assertIsArray($roster);
        self::assertSame(self::ids(224, 221, 222, 223, 226), array_column($roster, 'id'));
        self::assertIsArray($roster[0]);
        self::assertSame('2026-10-20T14:00:00.25Z', $roster[0]['createdAt']);
        self::assertSame('2026-10-21T14:00:00Z', $roster[0]['canceledAt']);
        self::assertSame(1900, $roster[0]['vatRate']);
        self::assertIsArray($roster[2]);
        self::assertSame('2026-10-26T17:00:00Z', $roster[2]['inviteExpiresAt']);
        self::assertIsArray($roster[3]);
        self::assertSame(['id' => self::id(3), 'name' => 'Zoe Alpha', 'anonymized' => true], $roster[3]['person']);
        self::assertIsArray($roster[4]);
        self::assertSame(['name' => 'Guest'], $roster[4]['guest']);
        self::assertArrayNotHasKey('person', $roster[4]);

        $mine = self::get($app, '/my/events', $member)['items'];
        self::assertIsArray($mine);
        $registrations = array_column($mine, 'registration');
        self::assertSame(self::ids(221, 222, 223, 227, 224), array_column($registrations, 'id'));
        self::assertSame([true, false, false, false, false], array_column($registrations, 'cancelable'));
        self::assertIsArray($registrations[1]);
        self::assertSame('2026-10-26T17:00:00Z', $registrations[1]['expiresAt']);
        foreach (['eventId', 'vatRate', 'guest', 'updatedAt', 'inviteExpiresAt'] as $officeField) {
            self::assertArrayNotHasKey($officeField, $registrations[1]);
        }
        self::assertSame(['items' => []], self::get($app, '/my/events', $office));
        self::get($app, '/my/events', status: 401);

        $link = '/event-invites/222';
        $request = (new ServerRequestFactory())->createServerRequest('GET', '/api/v1' . $link);
        $preview = $app->handle($request);
        self::assertSame(200, $preview->getStatusCode());
        self::assertSame('no-store', $preview->getHeaderLine('Cache-Control'));
        $document = self::get($app, $link);
        self::assertSame('Ari', $document['givenName']);
        self::assertSame('2026-10-26T17:00:00Z', $document['expiresAt']);
        self::assertIsArray($document['event']);
        self::assertSame('winter-workshop', $document['event']['slug']);
        self::assertSame(5, $document['event']['freePlaces']);
        self::assertSame($roster, self::get($app, $rosterPath, $office)['items']);
        self::get($app, '/event-invites/223', status: 404);
        self::get($app, '/event-invites/wrong-token', status: 404);
        self::get($this->app('2026-10-26T17:00:00Z'), $link, status: 404);
        $this->database->executeStatement("UPDATE events SET signup_closes_at = '2026-10-25 16:00:00' WHERE slug = 'winter-workshop'");
        self::get($app, $link, status: 404);
        $this->database->executeStatement(
            "UPDATE event_registrations SET status = 'registered', invite_token_hash = NULL, invited_at = NULL, expires_at = NULL "
            . "WHERE expires_at = '2026-10-26 17:00:00'",
        );
        $this->database->executeStatement("UPDATE events SET signup_closes_at = NULL WHERE slug = 'winter-workshop'");
        self::get($app, $link, status: 404);
    }

    public function testEventInviteExpiryQueuesTheNextReachableOfferInCron(): void
    {
        $this->database->executeStatement("UPDATE events SET capacity = 3, signup_closes_at = '2026-10-25 18:00:00' WHERE slug = 'winter-workshop'");
        $this->database->executeStatement("UPDATE accounts SET confirmed_at = '2026-10-01 00:00:00', locale = 'de' WHERE email = 'member@example.test'");
        foreach ([224 => 4, 225 => 6, 226 => 7] as $registration => $person) {
            $this->insert('event_registrations', [
                'id' => $registration, 'event_id' => 120, 'person_id' => $person, 'status' => 'waiting', 'payment' => 'paid',
                'price' => 0, 'vat_rate_bp' => 0, 'created_at' => '2026-10-24 14:00:00', 'updated_at' => '2026-10-24 14:00:00',
            ]);
        }
        $pk = $this->postgres ? '\x' . str_repeat('04', 65) : str_repeat("\x04", 65);
        $secret = $this->postgres ? '\x' . str_repeat('05', 16) : str_repeat("\x05", 16);
        $this->insert('push_subscriptions', [
            'id' => 301, 'account_id' => 10, 'endpoint' => 'https://push.example.test/device',
            'p256dh' => $pk, 'auth' => $secret, 'created_at' => '2026-10-23 10:00:00', 'last_used_at' => '2026-10-23 10:00:00',
        ]);
        $baseUrl = 'https://school.example.test/dance/schedule';
        $jobs = $this->eventExpiryJobs(new DateTimeImmutable(self::BEFORE_RELEASE));
        self::assertSame(JobResult::Ok, $jobs->expireEventInvites($baseUrl, pushEnabled: true));
        self::assertNull($jobs->expireEventInvites($baseUrl, pushEnabled: true));
        self::assertSame(['ok'], $this->database->fetchFirstColumn("SELECT result FROM job_runs WHERE job = 'waitlist-expiry'"));

        $office = new SessionPrincipal(self::id(11), self::id(5), 'office', null, null);
        $app = $this->app(self::BEFORE_RELEASE);
        $roster = self::get($app, '/admin/events/' . self::id(120) . '/registrations', $office)['items'];
        self::assertIsArray($roster);
        self::assertSame(self::ids(221, 222, 223, 224, 225, 226), array_column($roster, 'id'));
        self::assertSame(['registered', 'invited', 'canceled', 'waiting', 'invited', 'waiting'], array_column($roster, 'status'));
        self::assertIsArray($roster[2]);
        self::assertSame(self::BEFORE_RELEASE, $roster[2]['canceledAt']);
        self::assertIsArray($roster[4]);
        self::assertSame('2026-10-25T18:00:00Z', $roster[4]['inviteExpiresAt']);
        self::assertSame([], $this->database->fetchFirstColumn("SELECT id FROM event_registrations WHERE status = 'canceled' AND invite_token_hash IS NOT NULL"));

        $mail = $this->database->fetchAllAssociative('SELECT template, recipient, body_text FROM outbox_mails ORDER BY template');
        self::assertSame(['event-invite', 'event-invite-expired'], array_column($mail, 'template'));
        self::assertSame(['member@example.test', 'member@example.test'], array_column($mail, 'recipient'));
        self::assertIsString($mail[0]['body_text']);
        self::assertStringContainsString('Nia Zebra', $mail[0]['body_text']);
        $found = preg_match('~https://school\.example\.test/dance/schedule/events/invite\?token=([A-Za-z0-9_-]{43})~', $mail[0]['body_text'], $parts);
        self::assertSame(1, $found);
        $document = self::get($app, '/event-invites/' . $parts[1]);
        self::assertSame('Nia', $document['givenName']);
        self::get($app, '/event-invites/223', status: 404);

        $payload = $this->database->fetchOne('SELECT payload FROM push_outbox');
        self::assertIsString($payload);
        $note = json_decode($payload, true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($note);
        self::assertSame($parts[0], $note['url']);
        self::assertSame('event-invite-' . self::id(120), $note['tag']);
        self::assertSame('de', $note['lang']);
        self::assertIsString($note['body']);
        self::assertStringNotContainsString('Nia', $note['body']);

        $atClose = '2026-10-25T18:00:00Z';
        self::assertSame(JobResult::Ok, $this->eventExpiryJobs(new DateTimeImmutable($atClose))->expireEventInvites($baseUrl, pushEnabled: true));
        $closed = self::get($this->app($atClose), '/admin/events/' . self::id(120) . '/registrations', $office)['items'];
        self::assertIsArray($closed);
        self::assertSame(['registered', 'invited', 'canceled', 'waiting', 'canceled', 'waiting'], array_column($closed, 'status'));
        self::assertSame(['event-invite', 'event-invite-expired', 'event-invite-expired'], $this->database->fetchFirstColumn(
            'SELECT template FROM outbox_mails ORDER BY template',
        ));
        self::get($this->app($atClose), '/event-invites/' . $parts[1], status: 404);
    }

    public function testEventInviteExpiryRollsBackOneEventAndContinuesWithTheNext(): void
    {
        $this->database->executeStatement("UPDATE accounts SET confirmed_at = '2026-10-01 00:00:00' WHERE email = 'member@example.test'");
        $this->database->executeStatement("UPDATE persons SET email = 'invalid address' WHERE given_name = 'Zoe'");
        $this->insert('event_registrations', [
            'id' => 224, 'event_id' => 121, 'person_id' => 2, 'status' => 'invited', 'payment' => 'paid',
            'price' => 0, 'vat_rate_bp' => 0, 'created_at' => '2026-10-22 14:00:00', 'updated_at' => '2026-10-24 14:00:00',
            'invited_at' => '2026-10-24 14:00:00', 'expires_at' => '2026-10-25 16:59:59',
            'invite_token_hash' => hash('sha256', '224', true),
        ]);
        $jobs = $this->eventExpiryJobs(new DateTimeImmutable(self::BEFORE_RELEASE));
        self::assertSame(JobResult::Failed, $jobs->expireEventInvites('https://school.example.test/dance'));
        self::assertSame(['failed'], $this->database->fetchFirstColumn("SELECT result FROM job_runs WHERE job = 'waitlist-expiry'"));

        $office = new SessionPrincipal(self::id(11), self::id(5), 'office', null, null);
        $app = $this->app(self::BEFORE_RELEASE);
        $first = self::get($app, '/admin/events/' . self::id(120) . '/registrations', $office)['items'];
        $second = self::get($app, '/admin/events/' . self::id(121) . '/registrations', $office)['items'];
        self::assertIsArray($first);
        self::assertIsArray($second);
        self::assertSame(['registered', 'invited', 'invited'], array_column($first, 'status'));
        self::assertSame(['canceled'], array_column($second, 'status'));
        self::assertSame(['event-invite-expired'], $this->database->fetchFirstColumn('SELECT template FROM outbox_mails'));
        self::assertSame([], $this->database->fetchFirstColumn('SELECT id FROM push_outbox'));
    }

    public function testEventInviteLinkActionsConsumeTokensAndSettleExpiredOffers(): void
    {
        $this->database->executeStatement("UPDATE accounts SET confirmed_at = '2026-10-01 00:00:00', locale = 'de' WHERE email = 'member@example.test'");
        $this->database->executeStatement("UPDATE event_registrations SET price = 12500 WHERE expires_at = '2026-10-26 17:00:00'");
        $this->database->executeStatement("UPDATE events SET capacity = 3 WHERE slug = 'winter-workshop'");
        $this->insert('event_registrations', [
            'id' => 224, 'event_id' => 120, 'person_id' => 6, 'status' => 'waiting', 'payment' => 'paid',
            'price' => 0, 'vat_rate_bp' => 0, 'created_at' => '2026-10-24 14:00:00', 'updated_at' => '2026-10-24 14:00:00',
        ]);
        $this->database->executeStatement("UPDATE persons SET email = 'third@example.test' WHERE given_name = 'Third'");
        $this->insert('event_registrations', [
            'id' => 225, 'event_id' => 120, 'person_id' => 7, 'status' => 'waiting', 'payment' => 'paid',
            'price' => 0, 'vat_rate_bp' => 0, 'created_at' => '2026-10-24 14:00:00', 'updated_at' => '2026-10-24 14:00:00',
        ]);
        $app = $this->app(self::BEFORE_RELEASE);
        $respond = static function (string $token, string $action, int $status) use ($app): void {
            $request = (new ServerRequestFactory())->createServerRequest('POST', '/api/v1/event-invites/' . $token . '/' . $action);
            $response = $app->handle($request);
            self::assertSame($status, $response->getStatusCode(), (string) $response->getBody());
            self::assertSame('no-store', $response->getHeaderLine('Cache-Control'));
            if ($status === 204) {
                self::assertSame('', (string) $response->getBody());
            }
        };
        $respond('222', 'accept', 204);
        $respond('222', 'accept', 404);
        self::get($app, '/event-invites/222', status: 404);
        $mail = $this->database->fetchOne("SELECT body_text FROM outbox_mails WHERE template = 'event-registered'");
        self::assertIsString($mail);
        self::assertStringContainsString('Ari Zebra', $mail);
        self::assertStringContainsString("125,00\u{00a0}€", $mail);

        $respond('223', 'accept', 404);
        $offer = $this->database->fetchOne("SELECT body_text FROM outbox_mails WHERE template = 'event-invite'");
        self::assertIsString($offer);
        self::assertSame(1, preg_match('~https://school[.]example[.]test/dance/schedule/events/invite[?]token=([A-Za-z0-9_-]{43})~', $offer, $match));
        $token = $match[1] ?? '';
        self::assertNotSame('', $token);
        $invited = self::get($app, '/event-invites/' . $token);
        self::assertSame('Nia', $invited['givenName']);
        $respond($token, 'decline', 204);
        $respond($token, 'decline', 404);
        $next = $this->database->fetchOne("SELECT body_text FROM outbox_mails WHERE template = 'event-invite' AND recipient = 'third@example.test'");
        self::assertIsString($next);
        self::assertStringContainsString('Third Person', $next);

        $office = new SessionPrincipal(self::id(11), self::id(5), 'office', null, null);
        $roster = self::get($app, '/admin/events/' . self::id(120) . '/registrations', $office)['items'];
        self::assertIsArray($roster);
        self::assertSame(['registered', 'registered', 'canceled', 'canceled', 'invited'], array_column($roster, 'status'));
        self::assertSame(
            ['event-invite', 'event-invite', 'event-invite-expired', 'event-registered'],
            $this->database->fetchFirstColumn('SELECT template FROM outbox_mails ORDER BY template'),
        );
    }

    public function testSignedInEventInvitesCheckFamilyAndSettleUnderTheEventLock(): void
    {
        $this->database->executeStatement("UPDATE events SET capacity = 3 WHERE slug = 'winter-workshop'");
        $this->database->executeStatement("UPDATE accounts SET confirmed_at = '2026-10-01 00:00:00', locale = 'de' WHERE email = 'member@example.test'");
        $this->database->executeStatement("UPDATE persons SET email = 'third@example.test' WHERE given_name = 'Third'");
        foreach ([224 => 6, 225 => 7] as $id => $person) {
            $this->insert('event_registrations', [
                'id' => $id, 'event_id' => 120, 'person_id' => $person, 'status' => 'waiting', 'payment' => 'paid',
                'price' => 0, 'vat_rate_bp' => 0, 'created_at' => '2026-10-24 14:00:00', 'updated_at' => '2026-10-24 14:00:00',
            ]);
        }
        $app = $this->app(self::BEFORE_RELEASE);
        $member = new SessionPrincipal(self::id(10), self::id(1), 'member', impersonatorId: self::id(11));
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $invite = static fn(int $id, string $action): string => '/event-registrations/' . self::id($id) . '/' . $action;
        self::post($app, $invite(222, 'accept'), null, 401);
        self::post($app, $invite(222, 'accept'), $office, 404);
        self::post($app, '/event-registrations/invalid/accept', $member, 422);
        self::post($app, $invite(999, 'accept'), $member, 404);
        self::get($app, '/event-invites/222');
        $closed = $this->app('2026-10-26T15:00:00Z');
        $this->database->executeStatement("UPDATE events SET signup_closes_at = '2026-10-25 18:00:00' WHERE slug = 'winter-workshop'");
        self::assertSame('closed', self::post($closed, $invite(222, 'accept'), $member, 409)['reason']);
        $this->database->executeStatement("UPDATE events SET signup_closes_at = NULL WHERE slug = 'winter-workshop'");

        $accepted = self::post($app, $invite(222, 'accept'), $member, 200)['registration'];
        self::assertIsArray($accepted);
        self::assertSame('registered', $accepted['status']);
        self::assertTrue($accepted['cancelable']);
        self::assertArrayNotHasKey('expiresAt', $accepted);
        self::get($app, '/event-invites/222', status: 404);
        self::assertSame('invite-used', self::post($app, $invite(222, 'decline'), $member, 409)['reason']);
        $notice = $this->database->fetchAssociative("SELECT recipient, body_text FROM outbox_mails WHERE template = 'event-registered'");
        self::assertIsArray($notice);
        self::assertSame('member@example.test', $notice['recipient']);
        self::assertIsString($notice['body_text']);
        self::assertStringContainsString('Ari Zebra', $notice['body_text']);

        self::assertSame('invite-expired', self::post($app, $invite(223, 'leave'), $member, 409)['reason']);
        $declined = self::post($app, $invite(224, 'decline'), $member, 200)['registration'];
        self::assertIsArray($declined);
        self::assertSame('canceled', $declined['status']);
        self::assertSame(self::BEFORE_RELEASE, $declined['canceledAt']);
        self::assertSame('invite-expired', self::post($app, $invite(224, 'accept'), $member, 409)['reason']);
        $roster = self::get($app, '/admin/events/' . self::id(120) . '/registrations', $office)['items'];
        self::assertIsArray($roster);
        self::assertSame(['registered', 'registered', 'canceled', 'canceled', 'invited'], array_column($roster, 'status'));
        self::assertSame(
            ['event-invite', 'event-invite', 'event-invite-expired', 'event-registered'],
            $this->database->fetchFirstColumn('SELECT template FROM outbox_mails ORDER BY template'),
        );

        $actor = $this->postgres ? self::id(11) : hex2bin(str_replace('-', '', self::id(11)));
        $audit = $this->database->fetchAllAssociative('SELECT actor_id, action, target_kind FROM audit_events ORDER BY action');
        self::assertSame([
            ['actor_id' => $actor, 'action' => 'event-registration-added', 'target_kind' => 'person'],
            ['actor_id' => $actor, 'action' => 'event-registration-canceled', 'target_kind' => 'person'],
        ], $audit);
    }

    public function testEventSignupAndDeskRegistrationValidateAndRespectCapacity(): void
    {
        $app = $this->app(self::BEFORE_RELEASE);
        $member = new SessionPrincipal(self::id(10), self::id(1), 'member');
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $admin = '/admin/events/' . self::id(120) . '/registrations';
        $signup = '/events/winter-workshop/registrations';
        $waitlist = '/events/winter-workshop/waitlist';
        self::post($app, $admin, $member, 403, []);
        self::post($app, $admin, $office, 422, []);
        self::post($app, $admin, $office, 422, ['guest' => true]);
        self::post($app, $admin, $office, 422, ['guest' => ['nmae' => 'Misspelled']]);
        self::post($app, $admin, $office, 422, ['guest' => ['name' => 42]]);
        self::post($app, $admin, $office, 422, ['guest' => ['name' => '   ', 'email' => 'guest@example.test']]);
        self::post($app, $admin, $office, 422, ['guest' => ['name' => 'Desk guest']]);
        self::post($app, $admin, $office, 422, ['personId' => self::id(998)]);
        $draft = '/admin/events/' . self::id(121) . '/registrations';
        self::assertSame('closed', self::post($app, $draft, $office, 409, ['personId' => self::id(4)])['reason']);
        self::post($app, '/events/draft-workshop/registrations', $member, 404, ['personId' => self::id(6)]);
        self::post($app, $signup, $member, 422, []);
        self::post($app, $signup, $member, 404, ['personId' => self::id(4)]);
        self::assertSame('not-full', self::post($app, $waitlist, $member, 409, ['personId' => self::id(6)])['reason']);

        $desk = self::post($app, $admin, $office, 201, ['personId' => self::id(4)]);
        self::assertSame('registered', $desk['status']);
        self::assertSame('paid', $desk['payment']);
        $registered = self::post($app, $signup, $member, 201, ['personId' => self::id(6)])['registration'];
        self::assertIsArray($registered);
        self::assertSame('registered', $registered['status']);
        self::assertSame('paid', $registered['payment']);
        self::assertSame('duplicate', self::post($app, $signup, $member, 409, ['personId' => self::id(6)])['reason']);

        $this->database->executeStatement("UPDATE events SET capacity = 4 WHERE slug = 'winter-workshop'");
        $person = ['personId' => self::id(3)];
        self::assertSame('full', self::post($app, $signup, $member, 409, $person)['reason']);
        $waiting = self::post($app, $waitlist, $member, 201, $person)['registration'];
        self::assertIsArray($waiting);
        self::assertSame('waiting', $waiting['status']);
        self::assertIsString($waiting['id']);
        $change = '/event-registrations/' . $waiting['id'];
        self::assertSame('not-invited', self::post($app, $change . '/accept', $member, 409)['reason']);
        self::post($app, $change . '/leave', $member, 200);
        self::assertSame('not-waiting', self::post($app, $change . '/leave', $member, 409)['reason']);

        $guest = ['guest' => ['name' => ' Desk guest ', 'email' => 'guest@example.test']];
        self::assertSame('full', self::post($app, $admin, $office, 409, $guest)['reason']);
        $added = self::post($app, $admin, $office, 201, $guest + ['confirmOverCapacity' => true]);
        self::assertSame(['name' => 'Desk guest', 'email' => 'guest@example.test'], $added['guest']);
        self::assertIsString($added['id']);
        self::post($app, '/event-registrations/' . $added['id'] . '/accept', $member, 404);
        self::assertSame(['event-registered'], $this->database->fetchFirstColumn('SELECT template FROM outbox_mails'));
    }

    /**
     * @param App<ContainerInterface|null> $app
     * @param array<string,mixed>|null $body
     * @return array<array-key,mixed>
     */
    private static function post(App $app, string $path, ?SessionPrincipal $principal, int $status, ?array $body = null): array
    {
        return self::send($app, 'POST', $path, $principal, $status, $body);
    }

    /**
     * @param App<ContainerInterface|null> $app
     * @param array<array-key,mixed>|null $body
     * @return array<array-key,mixed>
     */
    private static function send(App $app, string $method, string $path, ?SessionPrincipal $principal, int $status, ?array $body = null): array
    {
        $request = (new ServerRequestFactory())->createServerRequest($method, '/api/v1' . $path)
            ->withAttribute('sessionPrincipal', $principal);
        if ($body !== null) {
            $request = $request->withHeader('Content-Type', 'application/json')
                ->withBody((new StreamFactory())->createStream(json_encode((object) $body, JSON_THROW_ON_ERROR)));
        }
        $response = $app->handle($request);
        $json = (string) $response->getBody();
        self::assertSame($status, $response->getStatusCode(), $json);
        $result = json_decode($json, true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($result);

        return $result;
    }

    public function testEventPaymentIssuesAnImmutablePdfAndDoesNotSkipNumbersAfterRenderFailure(): void
    {
        $registrationPath = '/admin/event-registrations/' . self::id(221) . '/pay';
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office', impersonatorId: self::id(10));
        $now = '2026-12-31T23:30:00Z';
        $app = $this->app($now);
        $this->database->executeStatement("UPDATE event_registrations SET price = 2250, vat_rate_bp = 1900 WHERE status = 'registered'");
        $pay = ['paymentMethod' => 'transfer', 'payerId' => self::id(6), 'recipientEmail' => 'receipt-payer@example.test'];
        self::post($app, $registrationPath, $office, 422, ['paymentMethod' => 'other']);
        self::post($app, '/admin/event-registrations/' . self::id(999) . '/pay', $office, 404, []);
        self::assertSame('not-registered', self::post($app, '/admin/event-registrations/' . self::id(222) . '/pay', $office, 409, [])['reason']);
        self::assertSame('receipt-settings-incomplete', self::post($app, $registrationPath, $office, 409, $pay)['reason']);
        $this->database->executeStatement(
            "UPDATE settings SET issuer_name = 'Fixture Studio', issuer_address = 'Room 1', issuer_tax_id = '123', tax_mode = 'standard'",
        );
        self::post($app, $registrationPath, $office, 422, ['payerId' => self::id(998)]);
        self::post($app, $registrationPath, $office, 422, ['recipientEmail' => 'bad address', 'payerId' => self::id(6)]);
        $fails = $this->app($now, renderReceipt: static fn(): never => throw new RuntimeException('Fixture PDF rendering error'));
        try {
            self::post($fails, $registrationPath, $office, 200, $pay);
            self::fail('Expected a rendering error to abort the payment');
        } catch (RuntimeException $error) {
            self::assertSame('Fixture PDF rendering error', $error->getMessage());
        }
        foreach (['receipts', 'receipt_counters', 'receipt_lines', 'receipt_pdfs', 'outbox_mails', 'audit_events'] as $table) {
            self::assertSame([], $this->database->fetchFirstColumn("SELECT 1 FROM $table"), $table);
        }
        self::assertSame(['unpaid'], $this->database->fetchFirstColumn("SELECT payment FROM event_registrations WHERE status = 'registered'"));

        $paid = self::post($app, $registrationPath, $office, 200, $pay);
        $receipt = $paid['receipt'];
        self::assertIsArray($receipt);
        self::assertSame('R-2027-0001', $receipt['number']);
        self::assertSame('2027-01-01', $receipt['issueDate']);
        self::assertIsArray($paid['registration']);
        self::assertSame('paid', $paid['registration']['payment']);
        self::assertSame($receipt['id'], $paid['registration']['receiptId']);
        self::assertSame('already-paid', self::post($app, $registrationPath, $office, 409, $pay)['reason']);
        $amounts = $this->database->fetchNumeric('SELECT net, vat, gross FROM receipts');
        self::assertIsArray($amounts);
        self::assertSame([1891, 359, 2250], array_map(static fn(mixed $value): int => is_numeric($value) ? (int) $value : -1, $amounts));
        self::assertSame(['receipt-payer@example.test'], $this->database->fetchFirstColumn(
            'SELECT m.recipient FROM outbox_mails m JOIN receipts r ON r.id = m.receipt_id',
        ));
        self::assertSame(['event-registration-paid'], $this->database->fetchFirstColumn('SELECT action FROM audit_events'));
        self::assertSame(
            $this->postgres ? self::id(10) : hex2bin(str_replace('-', '', self::id(10))),
            $this->database->fetchOne('SELECT created_by FROM receipts'),
        );
        self::assertIsString($receipt['id']);
        $attachment = (new \StudioSchedule\Receipt\AttachmentStore($this->database))->find($receipt['id']);
        self::assertIsArray($attachment);
        self::assertStringStartsWith('%PDF-', $attachment['pdf']);

        $this->insert('event_registrations', [
            'id' => 224, 'event_id' => 120, 'guest_name' => 'Guest', 'status' => 'registered',
            'payment' => 'unpaid', 'price' => 2250, 'vat_rate_bp' => 1900,
            'created_at' => '2026-10-22 14:00:00', 'updated_at' => '2026-10-22 14:00:00',
        ]);
        $guest = self::post($app, '/admin/event-registrations/' . self::id(224) . '/pay', $office, 200, [])['receipt'];
        self::assertIsArray($guest);
        self::assertSame('R-2027-0002', $guest['number']);
        self::assertSame(['receipt-payer@example.test'], $this->database->fetchFirstColumn('SELECT recipient FROM outbox_mails'));
    }

    public function testPassSaleRollsBackTheReceiptNumberAndPdfOnFailure(): void
    {
        $this->insert('pass_types', [
            'id' => 301, 'name' => 'Ten visits', 'kind' => 'visits', 'visits' => 10, 'validity_days' => 0,
            'price' => 2250, 'vat_rate_bp' => 1900, 'active' => true, 'created_at' => '2026-12-01 13:00:00',
        ]);
        $now = '2026-12-31T23:30:00Z';
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office', impersonatorId: self::id(10));
        $sell = '/admin/persons/' . self::id(2) . '/passes/sell';
        $pay = ['passTypeId' => self::id(301), 'payerId' => self::id(1), 'recipientEmail' => 'ignored@example.test'];
        $app = $this->app($now);
        self::assertSame('receipt-settings-incomplete', self::post($app, $sell, $office, 409, $pay)['reason']);

        $this->database->executeStatement(
            "UPDATE settings SET issuer_name = 'Fixture Studio', issuer_address = 'Room 1', issuer_tax_id = '123', tax_mode = 'standard'",
        );
        self::post($app, $sell, $office, 422, ['passTypeId' => self::id(999)]);
        self::post($app, $sell, $office, 422, ['passTypeId' => self::id(301), 'payerId' => self::id(999)]);
        self::post($app, '/admin/persons/' . self::id(999) . '/passes/assign', $office, 404, ['passTypeId' => self::id(301)]);

        $fails = $this->app($now, renderReceipt: static fn(): never => throw new RuntimeException('Fixture pass receipt render failure'));
        try {
            self::post($fails, $sell, $office, 201, $pay);
            self::fail('Expected the render failure to roll back the sale');
        } catch (RuntimeException $error) {
            self::assertSame('Fixture pass receipt render failure', $error->getMessage());
        }
        foreach (['passes', 'receipts', 'receipt_counters', 'receipt_lines', 'receipt_pdfs', 'outbox_mails'] as $table) {
            self::assertSame([], $this->database->fetchFirstColumn("SELECT 1 FROM $table"), $table);
        }

        $concurrent = DriverManager::getConnection($this->database->getParams());
        try {
            $concurrent->executeStatement($this->postgres ? "SET lock_timeout = '500ms'" : 'SET SESSION innodb_lock_wait_timeout = 1');
            $saleWithConcurrentRetirement = $this->app($now, renderReceipt: function (array $document, string $logo) use ($concurrent): string {
                try {
                    $concurrent->executeStatement('UPDATE pass_types SET active = FALSE');
                    self::fail('Pass type could be retired while a sale used its terms');
                } catch (DriverException $error) {
                    if ($this->postgres) {
                        self::assertSame('55P03', $error->getSQLState());
                    } else {
                        self::assertSame(1205, (int) $error->getCode());
                    }
                }

                return (new \StudioSchedule\Receipt\Renderer())->render($document, $logo);
            });
            $sale = self::post($saleWithConcurrentRetirement, $sell, $office, 201, $pay);
        } finally {
            $concurrent->close();
        }
        self::assertSame(10, $sale['visitsRemaining']);
        self::assertSame('2027-01-01', $sale['validFrom']);
        self::assertTrue($sale['usable']);
        self::assertArrayNotHasKey('validUntil', $sale);
        self::assertIsArray($sale['payer']);
        self::assertSame(self::id(1), $sale['payer']['id'] ?? null);
        self::assertSame(['R-2027-0001'], $this->database->fetchFirstColumn('SELECT number FROM receipts'));
        self::assertSame(['member@example.test'], $this->database->fetchFirstColumn('SELECT recipient FROM outbox_mails'));
        self::assertNull($this->database->fetchOne('SELECT recipient_email FROM receipts'));
        $actor = $this->postgres ? self::id(10) : hex2bin(str_replace('-', '', self::id(10)));
        self::assertSame($actor, $this->database->fetchOne('SELECT created_by FROM receipts'));
        self::assertSame($actor, $this->database->fetchOne('SELECT sold_by FROM passes'));
        self::assertIsString($sale['receiptId']);
        $attachment = (new \StudioSchedule\Receipt\AttachmentStore($this->database))->find($sale['receiptId']);
        self::assertIsArray($attachment);
        self::assertStringStartsWith('%PDF-', $attachment['pdf']);

        $this->database->executeStatement('UPDATE pass_types SET active = FALSE');
        self::post($app, $sell, $office, 422, $pay);
        self::assertSame(['R-2027-0001'], $this->database->fetchFirstColumn('SELECT number FROM receipts'));
    }

    public function testVoidingAPassMovesOnlyFuturePaymentsUsingTheLessonSchoolDate(): void
    {
        $this->insert('pass_types', [
            'id' => 301, 'name' => 'Visits', 'kind' => 'visits', 'visits' => 3, 'validity_days' => 0,
            'price' => 0, 'vat_rate_bp' => 0, 'active' => true, 'created_at' => '2026-10-01 10:00:00',
        ]);
        foreach ([302 => [3, 0, null], 303 => [3, 3, '2026-10-28']] as $id => [$total, $left, $until]) {
            $this->insert('passes', [
                'id' => $id, 'pass_type_id' => 301, 'person_id' => 3, 'kind' => 'visits', 'origin' => 'assignment',
                'visits_total' => $total, 'visits_remaining' => $left, 'status' => 'active', 'valid_from' => '2026-10-01',
                'valid_until' => $until, 'price_paid' => 0, 'sold_at' => '2026-10-01 10:00:00',
            ]);
        }
        foreach ([304 => 101, 305 => 102] as $id => $lesson) {
            $this->insert('bookings', [
                'id' => $id, 'lesson_id' => $lesson, 'person_id' => 3, 'source' => 'self',
                'booked_at' => '2026-10-22 14:00:00', 'status' => 'booked', 'trial' => false,
            ]);
        }
        foreach ([202, 304, 305] as $booking) {
            $this->insert('pass_redemptions', ['pass_id' => 302, 'booking_id' => $booking, 'at' => '2026-10-25 18:00:00']);
        }
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $path = '/admin/passes/' . self::id(302) . '/void';
        $app = $this->app('2026-10-26T12:00:00Z');
        $pass = self::post($app, $path, $office, 200);
        self::assertSame('voided', $pass['status']);
        self::assertFalse($pass['usable']);
        self::assertSame(2, $pass['visitsRemaining']);
        self::assertSame(1, $pass['useCount']);
        $uses = $pass['uses'] ?? null;
        self::assertIsArray($uses);
        self::assertIsArray($uses[0] ?? null);
        self::assertSame(self::id(202), $uses[0]['bookingId'] ?? null);

        $encoded = fn(int $id): string|false => $this->postgres ? self::id($id) : hex2bin(str_replace('-', '', self::id($id)));
        self::assertSame([$encoded(302), $encoded(303)], $this->database->fetchFirstColumn('SELECT pass_id FROM pass_redemptions ORDER BY booking_id'));
        self::assertSame([$encoded(202), $encoded(304)], $this->database->fetchFirstColumn('SELECT booking_id FROM pass_redemptions ORDER BY booking_id'));
        self::assertSame([2, 2], array_map(
            static fn(mixed $value): int => is_numeric($value) ? (int) $value : -1,
            $this->database->fetchFirstColumn('SELECT visits_remaining FROM passes ORDER BY id'),
        ));
        self::assertSame('already-voided', self::post($app, $path, $office, 409)['reason']);
    }

    public function testCancelingPassReceiptRollsBackTheVoidAndCounterOnRenderFailure(): void
    {
        $this->database->executeStatement(
            "UPDATE settings SET issuer_name = 'Fixture Studio', issuer_address = 'Room 1', issuer_tax_id = '123', tax_mode = 'standard'",
        );
        $this->insert('pass_types', [
            'id' => 301, 'name' => 'Visits', 'kind' => 'visits', 'visits' => 3, 'validity_days' => 0,
            'price' => 1350, 'vat_rate_bp' => 1900, 'active' => true, 'created_at' => '2026-10-01 10:00:00',
        ]);
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $now = '2026-10-26T12:00:00Z';
        $app = $this->app($now);
        $pass = self::post($app, '/admin/persons/' . self::id(1) . '/passes/sell', $office, 201, ['passTypeId' => self::id(301)]);
        $receipt = $pass['receiptId'] ?? null;
        self::assertIsString($receipt);
        $path = '/admin/receipts/' . $receipt . '/cancel';
        $reason = ['reason' => '  Sold to the wrong person  '];
        $fails = $this->app($now, renderReceipt: static fn(): never => throw new RuntimeException('Fixture cancellation PDF failure'));
        try {
            self::post($fails, $path, $office, 201, $reason);
            self::fail('Expected the render failure to roll back the cancellation');
        } catch (RuntimeException $error) {
            self::assertSame('Fixture cancellation PDF failure', $error->getMessage());
        }
        self::assertSame(['active'], $this->database->fetchFirstColumn('SELECT status FROM passes'));
        self::assertSame(['R-2026-0001'], $this->database->fetchFirstColumn('SELECT number FROM receipts'));

        $credit = self::post($app, $path, $office, 201, $reason);
        self::assertSame('R-2026-0002', $credit['number']);
        self::assertSame('Sold to the wrong person', $credit['reason']);
        self::assertSame(-1350, $credit['gross']);
        self::assertSame(['voided'], $this->database->fetchFirstColumn('SELECT status FROM passes'));
        self::assertSame(['member@example.test', 'member@example.test'], $this->database->fetchFirstColumn('SELECT recipient FROM outbox_mails'));
        self::assertSame('already-canceled', self::post($app, $path, $office, 409, [])['reason']);
    }

    public function testCancelingAnEventReceiptRequiresAnEndedRegistrationBeforeTheFirstSession(): void
    {
        $this->database->executeStatement(
            "UPDATE settings SET issuer_name = 'Fixture Studio', issuer_address = 'Room 1', issuer_tax_id = '123', tax_mode = 'standard'",
        );
        $this->database->executeStatement("UPDATE event_registrations SET price = 1500 WHERE status = 'registered'");
        $registration = '/admin/event-registrations/' . self::id(221);
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $app = $this->app('2026-10-25T12:00:00Z');
        $paid = self::post($app, $registration . '/pay', $office, 200, []);
        self::assertIsArray($paid['receipt']);
        $receipt = $paid['receipt']['id'] ?? null;
        self::assertIsString($receipt);
        $path = '/admin/receipts/' . $receipt . '/cancel';
        self::assertSame('registration-open', self::post($app, $path, $office, 409, [])['reason']);
        self::assertSame('paid', self::post($app, $registration . '/cancel', $office, 200, ['refund' => false])['payment']);
        $afterStart = $this->app('2026-11-03T12:00:00Z');
        self::assertSame('event-attended', self::post($afterStart, $path, $office, 409, [])['reason']);
        $this->assertCancellationChecksTimeAfterLockingEvent($office, $receipt);
        $credit = self::post($app, $path, $office, 201, []);
        self::assertSame(self::id(120), $credit['eventId']);
        self::assertSame(-1500, $credit['gross']);
        self::assertSame(['refunded'], $this->database->fetchFirstColumn("SELECT payment FROM event_registrations WHERE status = 'canceled'"));
    }

    private function assertCancellationChecksTimeAfterLockingEvent(SessionPrincipal $office, string $receipt): void
    {
        $now = new DateTimeImmutable('2026-11-03T10:00:00Z');
        $clock = self::createStub(ClockInterface::class);
        $clock->method('now')->willReturnCallback(static function () use (&$now): DateTimeImmutable {
            return $now;
        });
        $advance = static function () use (&$now): void {
            $now = new DateTimeImmutable('2026-11-03T10:00:00.500000Z');
        };
        $logger = new class ($advance) extends AbstractLogger {
            /** @param Closure():void $advance */
            public function __construct(private readonly Closure $advance) {}

            /** @param array<string,mixed> $context */
            public function log(mixed $level, string|Stringable $message, array $context = []): void
            {
                $sql = $context['sql'] ?? null;
                if (is_string($sql) && str_contains($sql, 'SELECT id FROM events WHERE id = ? FOR UPDATE')) {
                    ($this->advance)();
                }
            }
        };
        $config = new Configuration();
        $config->setMiddlewares([new Middleware($logger)]);
        $writer = DriverManager::getConnection($this->database->getParams(), $config);
        $receipts = new ReceiptWriteStore($writer);
        $cancellation = new ReceiptCancellation(
            new EventStore($writer),
            new EventRegistrationStore($writer),
            new PassLedgerStore($writer),
            $receipts,
            new ReceiptReadStore($writer, $clock),
            new ReceiptIssuance($receipts, new Renderer()),
            $clock,
        );
        try {
            $cancellation->cancel($office, $receipt, '');
            self::fail('Receipt canceled after its first session started during the event lock');
        } catch (CatalogFailure $failure) {
            self::assertSame('event-attended', $failure->reason);
        } finally {
            $writer->close();
        }
    }

    public function testOfficeEventRefundKeepsOriginalAmountsAndRollsBackOnPdfFailure(): void
    {
        $this->database->executeStatement("UPDATE event_registrations SET price = 2250, vat_rate_bp = 1900 WHERE status = 'registered'");
        $this->database->executeStatement(
            "UPDATE settings SET issuer_name = 'Original Studio', issuer_address = 'Room 1', issuer_tax_id = '123', tax_mode = 'standard'",
        );
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $path = '/admin/event-registrations/' . self::id(221);
        $before = $this->app(self::BEFORE_RELEASE);
        $original = self::post($before, $path . '/pay', $office, 200, [])['receipt'];
        self::assertIsArray($original);
        self::assertIsString($original['id']);

        $after = '2026-11-06T12:00:00Z';
        $app = $this->app($after);
        $this->database->executeStatement("UPDATE settings SET issuer_name = NULL, tax_mode = 'small_business', default_locale = 'de'");
        $incomplete = self::post($app, $path . '/cancel', $office, 409, ['refund' => true]);
        self::assertSame('receipt-settings-incomplete', $incomplete['reason']);
        $this->database->executeStatement("UPDATE settings SET issuer_name = 'Current Studio'");
        $fails = $this->app($after, renderReceipt: static fn(): never => throw new RuntimeException('Fixture Storno rendering error'));
        try {
            self::post($fails, $path . '/cancel', $office, 200, ['refund' => true]);
            self::fail('Expected a rendering error to abort the refund');
        } catch (RuntimeException $error) {
            self::assertSame('Fixture Storno rendering error', $error->getMessage());
        }
        self::assertSame(['paid'], $this->database->fetchFirstColumn("SELECT payment FROM event_registrations WHERE status = 'registered'"));
        self::assertSame([$original['number']], $this->database->fetchFirstColumn('SELECT number FROM receipts'));
        self::assertSame(['event-registration-paid'], $this->database->fetchFirstColumn('SELECT action FROM audit_events'));
        self::assertSame(['receipt'], $this->database->fetchFirstColumn('SELECT template FROM outbox_mails'));

        $refund = self::post($app, $path . '/cancel', $office, 200, ['refund' => true]);
        self::assertSame('canceled', $refund['status']);
        self::assertSame('refunded', $refund['payment']);
        self::assertSame($original['id'], $refund['receiptId']);
        self::assertSame($after, $refund['canceledAt']);
        self::assertSame('already-canceled', self::post($app, $path . '/cancel', $office, 409, ['refund' => true])['reason']);

        $refundAmounts = $this->database->fetchNumeric("SELECT net, vat, gross FROM receipts WHERE kind = 'cancellation'");
        self::assertIsArray($refundAmounts);
        self::assertSame([-1891, -359, -2250], array_map(static fn(mixed $value): int => is_numeric($value) ? (int) $value : 0, $refundAmounts));
        self::assertSame(['R-2026-0002', 'Current Studio', 'standard', 'de'], $this->database->fetchNumeric(
            "SELECT number, issuer_name, tax_mode, locale FROM receipts WHERE kind = 'cancellation'",
        ));
        self::assertSame(['member@example.test'], $this->database->fetchFirstColumn(
            "SELECT m.recipient FROM outbox_mails m JOIN receipts r ON r.id = m.receipt_id WHERE r.kind = 'cancellation'",
        ));
        $details = $this->database->fetchOne("SELECT details FROM audit_events WHERE action = 'event-registration-canceled'");
        self::assertIsString($details);
        $decoded = json_decode($details, true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($decoded);
        self::assertTrue($decoded['refund']);
    }

    public function testCancelEventRollsBackSessionsAndVisitsUntilAllRefundsSucceed(): void
    {
        $this->database->executeStatement(
            "UPDATE settings SET issuer_name = 'Fixture Studio', issuer_address = 'Room 1', issuer_tax_id = '123', "
            . "tax_mode = 'standard', redemption_mode = 'booking'",
        );
        $this->database->executeStatement("UPDATE accounts SET confirmed_at = '2026-10-10 12:00:00' WHERE email = 'member@example.test'");
        $this->database->executeStatement("UPDATE event_registrations SET price = 2250, vat_rate_bp = 1900 WHERE status = 'registered'");
        $this->insert('event_registrations', [
            'id' => 224, 'event_id' => 120, 'guest_name' => 'Fixture Guest', 'guest_email' => 'MEMBER@EXAMPLE.TEST',
            'payment' => 'unpaid', 'status' => 'registered', 'price' => 2250, 'vat_rate_bp' => 1900,
            'created_at' => '2026-10-20 14:00:00', 'updated_at' => '2026-10-20 14:00:00',
        ]);
        foreach ([225 => ['ΟΣ@example.test', 'First Guest'], 226 => ['οσ@example.test', 'Second Guest']] as $id => [$email, $name]) {
            $this->insert('event_registrations', [
                'id' => $id, 'event_id' => 120, 'guest_name' => $name, 'guest_email' => $email,
                'payment' => 'unpaid', 'status' => 'registered', 'price' => 2250, 'vat_rate_bp' => 1900,
                'created_at' => '2026-10-21 14:00:00', 'updated_at' => '2026-10-21 14:00:00',
            ]);
        }
        $this->insert('pass_types', [
            'id' => 301, 'name' => 'Fixture Visits', 'kind' => 'visits', 'visits' => 5, 'validity_days' => 0,
            'price' => 0, 'vat_rate_bp' => 0, 'active' => true, 'created_at' => '2026-10-20 10:00:00',
        ]);
        $this->insert('passes', [
            'id' => 302, 'pass_type_id' => 301, 'person_id' => 1, 'kind' => 'visits',
            'visits_total' => 5, 'visits_remaining' => 4, 'valid_from' => '2026-10-20',
            'status' => 'active', 'origin' => 'assignment', 'price_paid' => 0, 'sold_at' => '2026-10-20 10:00:00',
        ]);
        $this->insert('bookings', [
            'id' => 303, 'lesson_id' => 106, 'person_id' => 1, 'status' => 'booked', 'trial' => false,
            'booked_at' => '2026-10-24 16:00:00',
        ]);
        $this->insert('pass_redemptions', ['pass_id' => 302, 'booking_id' => 303, 'at' => '2026-10-24 16:00:00']);
        $this->insert('waitlist_entries', [
            'id' => 304, 'lesson_id' => 106, 'person_id' => 6, 'status' => 'invited', 'stay_on_list' => true,
            'joined_at' => '2026-10-22 10:00:00', 'invited_at' => '2026-10-24 10:00:00',
            'expires_at' => '2026-10-26 10:00:00', 'invite_token_hash' => hash('sha256', 'cancel-legacy-lesson', true),
        ]);

        $office = new SessionPrincipal(self::id(11), self::id(5), 'office', impersonatorId: self::id(10));
        $app = $this->app(self::BEFORE_RELEASE);
        self::get($app, '/event-invites/222');
        $paid = self::post($app, '/admin/event-registrations/' . self::id(221) . '/pay', $office, 200, []);
        self::assertIsArray($paid['receipt']);
        $path = '/admin/events/' . self::id(120);
        $note = ['note' => '  Fixture venue closed  '];
        self::post($app, $path . '/cancel', $office, 422, ['note' => true]);
        self::post($app, '/admin/events/' . self::id(999) . '/cancel', $office, 404, []);

        $fails = $this->app(self::BEFORE_RELEASE, renderReceipt: static fn(): never => throw new RuntimeException('Fixture event refund failed'));
        try {
            self::post($fails, $path . '/cancel', $office, 200, $note);
            self::fail('Expected the refund to abort the whole event cancellation');
        } catch (RuntimeException $error) {
            self::assertSame('Fixture event refund failed', $error->getMessage());
        }
        $stillOpen = self::get($app, $path, $office);
        self::assertSame('published', $stillOpen['status']);
        self::assertSame(4, $stillOpen['registered']);
        self::assertIsArray($stillOpen['sessions']);
        self::assertSame(['scheduled'], array_column($stillOpen['sessions'], 'status'));
        self::assertContains($this->database->fetchOne('SELECT visits_remaining FROM passes'), [4, '4']);
        self::assertCount(1, $this->database->fetchFirstColumn('SELECT 1 FROM pass_redemptions'));
        self::assertSame(['receipt'], $this->database->fetchFirstColumn('SELECT template FROM outbox_mails'));
        self::assertSame(['event-registration-paid'], $this->database->fetchFirstColumn('SELECT action FROM audit_events'));

        $canceled = self::post($app, $path . '/cancel', $office, 200, $note);
        self::assertSame('canceled', $canceled['status']);
        self::assertIsArray($canceled['sessions']);
        self::assertSame(['canceled'], array_column($canceled['sessions'], 'status'));
        self::assertSame(0, $canceled['invited']);
        self::get($app, '/event-invites/222', status: 404);
        self::assertCount(0, $this->database->fetchFirstColumn("SELECT 1 FROM event_registrations WHERE status = 'canceled' AND invite_token_hash IS NOT NULL"));
        $lesson = $this->postgres ? self::id(106) : hex2bin(str_replace('-', '', self::id(106)));
        $idType = $this->postgres ? ParameterType::STRING : ParameterType::BINARY;
        $session = $this->database->fetchNumeric('SELECT cancel_note, canceled_by FROM lessons WHERE id = ?', [$lesson], [$idType]);
        self::assertSame(['Fixture venue closed', $this->postgres ? self::id(10) : hex2bin(str_replace('-', '', self::id(10)))], $session);
        self::assertContains($this->database->fetchOne('SELECT visits_remaining FROM passes'), [5, '5']);
        self::assertSame([], $this->database->fetchFirstColumn('SELECT 1 FROM pass_redemptions'));
        self::assertSame(['booked'], $this->database->fetchFirstColumn('SELECT status FROM bookings WHERE lesson_id = ?', [$lesson], [$idType]));
        $invite = $this->database->fetchNumeric(
            'SELECT status, invited_at, expires_at FROM waitlist_entries WHERE invite_token_hash IS NOT NULL AND lesson_id = ?',
            [$lesson],
            [$idType],
        );
        self::assertSame(['waiting', null, null], $invite);
        self::assertSame(['R-2026-0002'], $this->database->fetchFirstColumn("SELECT number FROM receipts WHERE kind = 'cancellation'"));
        self::assertSame(['event-canceled', 'event-registration-paid'], $this->database->fetchFirstColumn('SELECT action FROM audit_events ORDER BY action'));
        $notices = $this->database->fetchAllAssociative("SELECT recipient, body_text FROM outbox_mails WHERE template = 'event-canceled'");
        self::assertCount(2, $notices);
        $bodies = array_column($notices, 'body_text', 'recipient');
        $family = $bodies['member@example.test'];
        self::assertIsString($family);
        self::assertStringContainsString('Fixture Guest', $family);
        self::assertStringContainsString('Mara Steps', $family);
        self::assertStringContainsString('refunded', $family);
        $guests = $bodies['ΟΣ@example.test'];
        self::assertIsString($guests);
        self::assertStringContainsString('First Guest, Second Guest', $guests);
        self::assertSame('closed', self::post($app, $path . '/cancel', $office, 409, [])['reason']);

        $draft = self::post($app, '/admin/events/' . self::id(121) . '/cancel', $office, 200, []);
        self::assertSame('canceled', $draft['status']);
        self::assertCount(2, $this->database->fetchFirstColumn("SELECT 1 FROM outbox_mails WHERE template = 'event-canceled'"));
    }

    public function testOfficeLessonCancellationAndDeletionProtectExistingBookingsAndNotes(): void
    {
        $this->insertLesson(108, '2026-11-05', 'regular', 'public', ['class_id' => 40, 'slot_date' => '2026-11-05']);
        $this->insertLesson(109, '2026-11-05', 'workshop', 'public', []);
        $this->insertLesson(110, '2026-11-05', 'workshop', 'public', []);
        $this->insertLesson(111, '2026-10-24', 'workshop', 'public', []);
        $this->insert('bookings', [
            'id' => 250, 'lesson_id' => 109, 'status' => 'booked', 'trial' => false, 'booked_at' => '2026-10-24 14:00:00',
            'guest_name' => 'Lesson Guest', 'guest_email' => 'invalid address',
        ]);
        $this->insert('bookings', [
            'id' => 251, 'lesson_id' => 110, 'person_id' => 4, 'status' => 'canceled', 'trial' => false,
            'booked_at' => '2026-10-24 14:00:00', 'canceled_at' => '2026-10-24 14:30:00',
        ]);
        $this->insertLesson(112, '2026-11-07', 'workshop', 'public', []);
        $this->insert('waitlist_entries', [
            'id' => 252, 'lesson_id' => 112, 'person_id' => 4, 'status' => 'waiting', 'stay_on_list' => false,
            'joined_at' => '2026-10-24 10:00:00',
        ]);

        $office = new SessionPrincipal(self::id(11), self::id(5), 'office', impersonatorId: self::id(10));
        $wakes = 0;
        $wake = static function () use (&$wakes): void {
            ++$wakes;
        };
        $app = $this->app(self::BEFORE_RELEASE, wakeLessonMail: $wake);
        $path = static fn(int $id): string => '/lessons/' . self::id($id);
        $delete = static function (string $id, int $status) use ($app, $office): string {
            $request = (new ServerRequestFactory())->createServerRequest('DELETE', '/api/v1' . $id)
                ->withAttribute('sessionPrincipal', $office);
            $response = $app->handle($request);
            self::assertSame($status, $response->getStatusCode(), (string) $response->getBody());

            return (string) $response->getBody();
        };
        self::assertStringContainsString('"reason":"generated"', $delete($path(108), 409));
        self::assertStringContainsString('"reason":"in-use"', $delete($path(110), 409));
        self::assertStringContainsString('"location":"path.lessonId"', $delete($path(111), 422));
        self::assertStringContainsString('"reason":"in-use"', $delete($path(106), 409));
        $delete('/lessons/invalid', 422);
        $delete($path(999), 404);
        self::assertSame('', $delete($path(112), 204));
        self::get($app, $path(112), $office, 404);

        self::post($app, $path(111) . '/cancel', $office, 422, ['note' => 'Too late']);
        self::post($app, $path(108) . '/cancel', $office, 422, ['note' => str_repeat('ä', 501)]);
        $class = self::post($app, $path(108) . '/cancel', $office, 200, ['note' => '    ']);
        self::assertIsArray($class['admin']);
        self::assertTrue($class['admin']['detached']);
        self::assertArrayNotHasKey('cancelNote', $class['admin']);
        try {
            self::post($app, $path(109) . '/cancel', $office, 200, ['note' => 'Fixture cancellation']);
            self::fail('Expected invalid mail data to roll back the cancellation');
        } catch (RuntimeException $error) {
            self::assertSame('A lesson cancellation recipient has an invalid email address', $error->getMessage());
        }
        self::assertSame('scheduled', self::get($app, $path(109), $office)['status']);
        $this->database->executeStatement("UPDATE bookings SET guest_email = 'lesson-guest@example.test' WHERE guest_name = 'Lesson Guest'");
        $canceled = self::post($app, $path(109) . '/cancel', $office, 200, ['note' => ' Fixture cancellation ']);
        self::assertSame('canceled', $canceled['status']);
        self::assertIsArray($canceled['admin']);
        self::assertSame(1, $canceled['admin']['activeBookings']);
        self::assertSame('Fixture cancellation', $canceled['admin']['cancelNote']);
        self::assertSame(['id' => self::id(10), 'name' => 'Mara Steps'], $canceled['admin']['canceledBy']);
        self::assertStringContainsString('"reason":"in-use"', $delete($path(109), 409));
        $later = $this->app('2026-12-01T10:00:00Z', wakeLessonMail: $wake);
        $repeated = self::post($later, $path(109) . '/cancel', $office, 200, ['note' => 'Different note']);
        self::assertSame($canceled['admin'], $repeated['admin']);
        self::assertSame(1, $wakes);
        self::assertSame(['lesson-guest@example.test'], $this->database->fetchFirstColumn(
            "SELECT recipient FROM outbox_mails WHERE template = 'lesson-canceled'",
        ));
    }

    public function testEventSessionSignupPreservesAccessAndLeavesRegistrationsUnchanged(): void
    {
        $app = $this->app(self::BEFORE_RELEASE);
        $member = new SessionPrincipal(self::id(10), self::id(1), 'member');
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $instructor = new SessionPrincipal(self::id(11), self::id(5), 'instructor');
        $session = '/lessons/' . self::id(106);
        $draft = '/lessons/' . self::id(107);
        $ordinary = '/lessons/' . self::id(102);
        $memberData = ['personId' => self::id(1)];
        $childData = ['personId' => self::id(2), 'stayOnList' => true];
        $strangerData = ['personId' => self::id(4)];
        $guest = ['guest' => ['name' => 'Walk-in', 'email' => 'guest@example.test'], 'confirmOverCapacity' => true];
        $counts = fn(): array => array_map($this->database->fetchOne(...), [
            'SELECT COUNT(*) FROM bookings',
            'SELECT COUNT(*) FROM waitlist_entries',
            'SELECT COUNT(*) FROM event_registrations',
            'SELECT COUNT(*) FROM outbox_mails',
        ]);
        $before = $counts();

        self::post($app, $session . '/bookings', null, 401, $memberData);
        self::post($app, $session . '/participants', $member, 403, $guest);
        self::post($app, '/lessons/invalid/bookings', $member, 422, $memberData);
        self::post($app, $session . '/bookings', $member, 400);
        self::post($app, '/lessons/' . self::id(999) . '/bookings', $member, 404, $memberData);
        self::post($app, $session . '/bookings', $member, 404, $strangerData);
        self::post($app, $draft . '/waitlist', $member, 404, $childData);
        self::post($app, $draft . '/participants', $instructor, 404, $guest);
        foreach ([
            self::post($app, $session . '/bookings', $member, 409, $memberData),
            self::post($app, $session . '/waitlist', $member, 409, $childData),
            self::post($app, $draft . '/participants', $office, 409, $guest),
            self::post($app, $session . '/participants', $office, 409, [
                'guest' => ['name' => "Walk\0in"], 'trialNote' => "Fixture\0note",
            ]),
            self::post($app, $session . '/participants', $office, 409, $strangerData + ['trial' => true, 'trialNote' => 'First visit']),
        ] as $failure) {
            self::assertSame('event-session', $failure['reason']);
            self::assertArrayNotHasKey('why', $failure);
        }
        self::post($app, $session . '/participants', $office, 422, []);
        self::post($app, $session . '/participants', $office, 422, ['personId' => self::id(999)]);
        self::post($app, $ordinary . '/bookings', $member, 404, $memberData);
        self::post($app, $ordinary . '/participants', $office, 405, $guest);
        self::assertSame($before, $counts());

        $this->database->executeStatement('DELETE FROM settings');
        self::post($app, $session . '/bookings', $member, 404, $memberData);
    }

    public function testEditingAndRestoringALessonSettlesVisitsAndReoffersPlaces(): void
    {
        $this->database->executeStatement("UPDATE settings SET redemption_mode = 'booking', waitlist_invite_expiry = "
            . ($this->postgres ? "'8 days'" : '691200000000'));
        $this->database->executeStatement("UPDATE accounts SET confirmed_at = '2026-10-24 09:00:00'");
        $this->insert('pass_types', [
            'id' => 301, 'name' => 'Fixture Visits', 'kind' => 'visits', 'visits' => 5, 'validity_days' => 0,
            'price' => 0, 'vat_rate_bp' => 0, 'active' => true, 'created_at' => '2026-10-20 10:00:00',
        ]);
        $this->insert('pass_types', [
            'id' => 302, 'name' => 'Fixture Period', 'kind' => 'time', 'validity_days' => 14,
            'price' => 0, 'vat_rate_bp' => 0, 'active' => true, 'created_at' => '2026-10-20 10:00:00',
        ]);
        $assignment = ['person_id' => 1, 'status' => 'active', 'origin' => 'assignment', 'price_paid' => 0, 'sold_at' => '2026-10-20 10:00:00'];
        $this->insert('passes', [
            'id' => 303, 'pass_type_id' => 301, 'kind' => 'visits', 'visits_total' => 5,
            'visits_remaining' => 4, 'valid_from' => '2026-10-20', 'valid_until' => '2026-10-30',
        ] + $assignment);
        $this->insert('passes', [
            'id' => 304, 'pass_type_id' => 302, 'kind' => 'time', 'valid_from' => '2026-10-31', 'valid_until' => '2026-11-13',
        ] + $assignment);
        $this->insert('passes', [
            'id' => 305, 'pass_type_id' => 301, 'kind' => 'visits', 'visits_total' => 2, 'visits_remaining' => 2,
            'valid_from' => '2026-10-31',
        ] + $assignment);
        $booked = ['lesson_id' => 102, 'status' => 'booked', 'trial' => false, 'booked_at' => '2026-10-24 10:00:00'];
        $this->insert('bookings', ['id' => 306, 'person_id' => 1] + $booked);
        $this->insert('bookings', ['id' => 307, 'person_id' => 4] + $booked);
        $this->insert('bookings', ['id' => 308, 'guest_name' => 'Fixture Guest', 'guest_email' => 'invalid'] + $booked);
        $this->insert('pass_redemptions', ['pass_id' => 303, 'booking_id' => 306, 'at' => '2026-10-24 10:00:00']);
        foreach ([309 => 7, 310 => 2] as $entry => $person) {
            $this->insert('waitlist_entries', [
                'id' => $entry, 'lesson_id' => 102, 'person_id' => $person, 'status' => 'waiting',
                'stay_on_list' => true, 'joined_at' => '2026-10-24 12:00:00',
            ]);
        }
        $pk = $this->postgres ? '\x' . str_repeat('04', 65) : str_repeat("\x04", 65);
        $secret = $this->postgres ? '\x' . str_repeat('05', 16) : str_repeat("\x05", 16);
        $this->insert('push_subscriptions', [
            'id' => 311, 'account_id' => 10, 'endpoint' => 'https://push.example.test/device',
            'p256dh' => $pk, 'auth' => $secret, 'created_at' => '2026-10-23 10:00:00', 'last_used_at' => '2026-10-23 10:00:00',
        ]);

        $app = $this->app(self::BEFORE_RELEASE);
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $path = '/lessons/' . self::id(102);
        $data = [
            'date' => '2026-11-01', 'startTime' => '08:00', 'durationMinutes' => 60,
            'roomId' => self::id(21), 'instructorId' => self::id(23), 'capacity' => 4,
            'visibility' => 'public', 'kind' => 'workshop',
        ];
        try {
            self::send($app, 'PUT', $path, $office, 200, $data);
            self::fail('Expected an invalid recipient to roll back the edit');
        } catch (RuntimeException $error) {
            self::assertSame('A lesson update recipient has an invalid email address', $error->getMessage());
        }
        self::assertSame('2026-10-30T10:00:00.5Z', self::get($app, $path, $office)['startsAt']);
        self::assertSame([], $this->database->fetchFirstColumn('SELECT 1 FROM outbox_mails'));
        $paidKinds = fn(): array => $this->database->fetchFirstColumn('SELECT p.kind FROM pass_redemptions r JOIN passes p ON p.id = r.pass_id');
        $visitsRemaining = function (): array {
            /** @var list<int|numeric-string> $visits */
            $visits = $this->database->fetchFirstColumn('SELECT visits_remaining FROM passes WHERE visits_remaining IS NOT NULL ORDER BY visits_remaining');

            return array_map(intval(...), $visits);
        };
        self::assertSame(['visits'], $paidKinds());

        $this->database->executeStatement("UPDATE bookings SET guest_email = 'lesson-guest@example.test' WHERE guest_name = 'Fixture Guest'");
        $updated = self::send($app, 'PUT', $path, $office, 200, $data);
        self::assertSame('2026-11-01T07:00:00Z', $updated['startsAt']);
        self::assertSame(0, $updated['freePlaces']);
        self::assertSame(['time'], $paidKinds());
        self::assertSame([2, 5], $visitsRemaining());
        $push = $this->database->fetchFirstColumn('SELECT payload FROM push_outbox');
        self::assertCount(1, $push);
        self::assertIsString($push[0]);
        self::assertStringContainsString('/dance/schedule/waitlist/accept?token=', $push[0]);
        self::assertStringNotContainsString('Ari', $push[0]);

        $attempt = ['capacity' => 3] + $data;
        $error = self::send($app, 'PUT', $path, $office, 422, $attempt);
        self::assertStringContainsString('places reserved for the waiting list', json_encode($error, JSON_THROW_ON_ERROR));
        self::send($app, 'PUT', $path, $office, 200, ['date' => '2026-11-03'] + $data);
        self::assertSame(['time'], $paidKinds());
        self::send($app, 'PUT', $path, $office, 200, ['date' => '2026-11-14'] + $data);
        self::assertSame(['visits'], $paidKinds());
        self::assertSame([1, 5], $visitsRemaining());
        self::post($app, $path . '/cancel', $office, 200, ['note' => 'Room repairs']);
        self::assertSame([], $paidKinds());
        $restored = self::post($app, $path . '/restore', $office, 200);
        self::assertSame('scheduled', $restored['status']);
        self::assertSame(0, $restored['freePlaces']);
        self::assertIsArray($restored['admin']);
        self::assertArrayNotHasKey('cancelNote', $restored['admin']);
        self::assertSame(['visits'], $paidKinds());
        self::assertCount(2, $this->database->fetchFirstColumn('SELECT payload FROM push_outbox'));
        self::post($app, $path . '/restore', $office, 200, []);
        self::assertCount(2, $this->database->fetchFirstColumn("SELECT 1 FROM outbox_mails WHERE template = 'waitlist-invite'"));
    }

    public function testLessonEditValidationAndRestorationUseExistingScheduleRules(): void
    {
        $this->insertLesson(108, '2026-11-05', 'regular', 'public', ['class_id' => 40, 'slot_date' => '2026-11-05']);
        $this->insertLesson(109, '2026-11-06', 'workshop', 'public', []);
        $this->insertLesson(110, '2026-11-07', 'workshop', 'public', []);
        $this->insert('bookings', [
            'id' => 301, 'lesson_id' => 109, 'person_id' => 4, 'status' => 'booked', 'trial' => false, 'booked_at' => '2026-10-24 16:00:00',
        ]);
        $this->insert('event_registrations', [
            'id' => 224, 'event_id' => 120, 'guest_name' => 'Event Guest', 'guest_email' => 'event-guest@example.test',
            'status' => 'registered', 'payment' => 'paid', 'price' => 0, 'vat_rate_bp' => 0,
            'created_at' => '2026-10-22 11:00:00', 'updated_at' => '2026-10-22 11:00:00',
        ]);

        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $app = $this->app(self::BEFORE_RELEASE);
        $path = static fn(int $id): string => '/lessons/' . self::id($id);
        $edit = static fn(int $id, int $status, array $data): array => self::send($app, 'PUT', $path($id), $office, $status, $data);
        $fields = [
            'date' => '2026-11-05', 'startTime' => '07:00', 'durationMinutes' => 60,
            'roomId' => self::id(21), 'instructorId' => self::id(23), 'accompanistId' => self::id(24),
            'kind' => 'regular', 'visibility' => 'public', 'capacity' => 4,
        ];
        $rental = [
            'kind' => 'rental', 'capacity' => 0, 'renter' => 'Arts group', 'instructorId' => null,
            'accompanistId' => null, 'visibility' => 'hidden',
        ] + $fields;

        $edit(999, 404, $fields);
        $edit(100, 422, $fields);
        $edit(108, 422, $rental);
        $edit(108, 422, ['startTime' => '23:59', 'durationMinutes' => 2] + $fields);
        $edit(108, 422, ['roomId' => self::id(999)] + $fields);
        $edit(108, 409, ['date' => '2026-10-24'] + $fields);
        $changed = $edit(108, 200, $fields);
        self::assertIsArray($changed['admin']);
        self::assertTrue($changed['admin']['detached']);
        self::assertSame(self::id(40), $changed['classId']);
        $edit(109, 422, $rental);
        $edit(105, 422, $fields);

        $madeRental = $edit(110, 200, ['date' => '2026-11-09'] + $rental);
        self::assertSame('Arts group', $madeRental['renter']);
        self::post($app, $path(110) . '/cancel', $office, 200, []);
        self::post($app, $path(110) . '/restore', $office, 200);

        $session = ['kind' => 'workshop', 'capacity' => 7, 'date' => '2026-11-08'] + $fields;
        $edit(106, 422, ['capacity' => 6] + $session);
        $edit(106, 422, ['visibility' => 'members'] + $session);
        $edit(106, 422, ['kind' => 'private'] + $session);
        $edit(106, 200, $session);
        $edit(106, 200, ['date' => '2026-11-07'] + $session);
        self::post($app, $path(106) . '/cancel', $office, 200, ['note' => 'Fixture']);
        self::post($app, $path(106) . '/restore', $office, 200);
        $late = $this->app('2026-12-01T10:00:00Z');
        self::post($late, $path(103) . '/restore', $office, 422);
        self::post($late, $path(106) . '/restore', $office, 200);
        self::assertCount(1, $this->database->fetchFirstColumn("SELECT 1 FROM outbox_mails WHERE template = 'lesson-restored' AND recipient = 'event-guest@example.test'"));
        self::post($app, '/admin/events/' . self::id(120) . '/cancel', $office, 200, []);
        self::assertSame('closed', self::post($app, $path(106) . '/restore', $office, 409)['reason']);
    }

    public function testLessonNoticesUseTheActualSenderAndRollBackAllRecipients(): void
    {
        $this->database->executeStatement("UPDATE accounts SET confirmed_at = '2026-10-24 09:00:00'");
        $this->database->executeStatement("UPDATE staff SET person_id = (SELECT person_id FROM accounts WHERE role = 'office') WHERE kind = 'instructor'");
        $this->insert('accounts', [
            'id' => 12, 'person_id' => 7, 'email' => 'admin@example.test', 'role' => 'admin',
            'confirmed_at' => '2026-10-24 09:00:00',
        ]);
        foreach ([
            224 => ['Shared Address', 'MEMBER@EXAMPLE.TEST', 'registered'],
            225 => ['Event Guest', 'invalid', 'registered'],
            226 => ['Canceled Guest', 'canceled@example.test', 'canceled'],
        ] as $id => [$name, $email, $status]) {
            $this->insert('event_registrations', [
                'id' => $id, 'event_id' => 120, 'guest_name' => $name, 'guest_email' => $email,
                'status' => $status, 'payment' => 'paid', 'price' => 0, 'vat_rate_bp' => 0,
                'created_at' => '2026-10-22 12:00:00', 'updated_at' => '2026-10-22 12:00:00',
                'canceled_at' => $status === 'canceled' ? '2026-10-23 14:00:00' : null,
            ]);
        }
        $wakes = 0;
        $wake = static function () use (&$wakes): void {
            ++$wakes;
        };
        $app = $this->app(self::BEFORE_RELEASE, wakeLessonMail: $wake);
        $path = static fn(int $id): string => '/lessons/' . self::id($id) . '/notify';
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $instructor = new SessionPrincipal(self::id(11), self::id(5), 'instructor', impersonatorId: self::id(12));
        $unassigned = new SessionPrincipal(self::id(12), self::id(7), 'instructor');
        $member = new SessionPrincipal(self::id(10), self::id(1), 'member');
        $missing = new SessionPrincipal(self::id(998), self::id(998), 'office');
        $data = ['message' => "  Door\r\nopens\u{2029}early.  "];

        self::post($app, $path(106), null, 401, $data);
        self::post($app, $path(106), $member, 403, $data);
        self::post($app, $path(106), $unassigned, 404, $data);
        self::post($app, $path(106), $missing, 404, $data);
        self::post($app, $path(999), $office, 404, $data);
        self::post($app, '/lessons/not-an-id/notify', $office, 422, $data);
        self::post($app, $path(106), $office, 400);
        self::post($app, $path(106), $office, 422, ['message' => '    ']);
        self::post($app, $path(106), $office, 422, ['message' => str_repeat('ä', 1001)]);
        self::post($app, $path(106), $office, 422, ['message' => "hidden\u{202e}"]);
        self::assertSame(['recipients' => 0], self::post($app, $path(107), $office, 200, $data));
        try {
            self::post($app, $path(106), $instructor, 200, $data);
            self::fail('Expected invalid recipient to roll back the notice');
        } catch (RuntimeException $error) {
            self::assertSame('A lesson notice has an invalid mail address', $error->getMessage());
        }
        self::assertSame(0, $wakes);
        self::assertSame([], $this->database->fetchFirstColumn('SELECT 1 FROM lesson_notices'));
        self::assertSame([], $this->database->fetchFirstColumn('SELECT 1 FROM outbox_mails'));

        $this->database->executeStatement("UPDATE event_registrations SET guest_email = 'event-guest@example.test' WHERE guest_name = 'Event Guest'");
        self::assertSame(['recipients' => 2], self::post($app, $path(106), $instructor, 200, $data));
        self::assertSame(1, $wakes);
        self::assertSame(['admin@example.test'], $this->database->fetchFirstColumn(
            'SELECT a.email FROM lesson_notices n JOIN accounts a ON a.id = n.sender_id WHERE n.recipients = 2',
        ));
        $mails = $this->database->fetchAllAssociative("SELECT recipient, reply_to, body_text FROM outbox_mails WHERE template = 'lesson-notice' ORDER BY recipient");
        self::assertSame(['event-guest@example.test', 'member@example.test'], array_column($mails, 'recipient'));
        self::assertSame(['admin@example.test', 'admin@example.test'], array_column($mails, 'reply_to'));
        foreach ($mails as $mail) {
            self::assertIsString($mail['body_text']);
            self::assertStringContainsString('Third Person', $mail['body_text']);
            self::assertStringContainsString("Door\nopens\nearly.", $mail['body_text']);
        }
        self::assertStringContainsString('Shared Address', $mails[1]['body_text']);

        $this->database->executeStatement("UPDATE staff SET person_id = (SELECT person_id FROM accounts WHERE role = 'office') WHERE kind = 'accompanist'");
        $this->database->executeStatement("UPDATE staff SET active = FALSE WHERE kind = 'instructor'");
        $accompanist = new SessionPrincipal(self::id(11), self::id(5), 'instructor');
        self::assertSame(['recipients' => 1], self::post($app, $path(100), $accompanist, 200, ['message' => 'Past lesson notice']));
        $body = $this->database->fetchOne("SELECT body_text FROM outbox_mails WHERE reply_to = 'office@example.test'");
        self::assertIsString($body);
        self::assertStringContainsString('Studio Pianist', $body);

        $this->database->executeStatement('DELETE FROM settings');
        self::post($app, $path(106), $office, 404, $data);
    }

    public function testLessonNoticeLimitsUseSchoolMidnightAndTheRollingHour(): void
    {
        foreach ([301 => 106, 302 => 106, 303 => 106, 304 => 101, 305 => null] as $id => $lesson) {
            $this->insert('lesson_notices', [
                'id' => $id, 'lesson_id' => $lesson, 'sender_id' => 11, 'recipients' => 1,
                'sent_at' => $id === 301 ? '2026-10-24 23:30:00.500000' : '2026-10-25 00:15:00',
            ]);
        }
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $send = static function (App $app, int $lesson) use ($office): ResponseInterface {
            $path = '/api/v1/lessons/' . self::id($lesson) . '/notify';
            $request = (new ServerRequestFactory())->createServerRequest('POST', $path)
                ->withAttribute('sessionPrincipal', $office)->withHeader('Content-Type', 'application/json')
                ->withBody((new StreamFactory())->createStream('{"message":"Quota fixture"}'));

            return $app->handle($request);
        };

        $app = $this->app('2026-10-25T00:30:00.250000Z');
        $daily = $send($app, 106);
        self::assertSame(429, $daily->getStatusCode());
        self::assertSame('81000', $daily->getHeaderLine('Retry-After'));
        $hourly = $send($app, 102);
        self::assertSame(429, $hourly->getStatusCode());
        self::assertSame('1', $hourly->getHeaderLine('Retry-After'));
        $exact = $send($this->app('2026-10-25T00:30:00.500000Z'), 102);
        self::assertSame(429, $exact->getStatusCode());
        self::assertSame('1', $exact->getHeaderLine('Retry-After'));
        $after = $send($this->app('2026-10-25T00:30:00.501000Z'), 102);
        self::assertSame(200, $after->getStatusCode());
        self::assertSame('{"recipients":0}', (string) $after->getBody());
        self::assertCount(5, $this->database->fetchFirstColumn('SELECT 1 FROM lesson_notices'));
        self::assertSame([], $this->database->fetchFirstColumn('SELECT 1 FROM outbox_mails'));
    }

    public function testTeachingScheduleUsesActiveStaffAndSchoolDayAttendance(): void
    {
        $this->database->executeStatement("UPDATE staff SET person_id = (SELECT id FROM persons WHERE given_name = 'Iona') WHERE kind = 'instructor'");
        $this->database->executeStatement("UPDATE staff SET person_id = (SELECT id FROM persons WHERE given_name = 'Third') WHERE kind = 'accompanist'");
        $this->insertLesson(108, '2026-10-25', 'private', 'hidden', [
            'status' => 'canceled', 'starts_at' => '2026-10-25 12:00:00', 'ends_at' => '2026-10-25 13:00:00',
        ]);
        $this->insertLesson(109, '2026-10-25', 'workshop', 'public', [
            'starts_at' => '2026-10-25 22:30:00', 'ends_at' => '2026-10-26 01:00:00',
        ]);
        $guest = ['lesson_id' => 100, 'trial' => true, 'booked_at' => '2026-10-22 14:00:00'];
        $this->insert('bookings', ['id' => 301, 'guest_name' => 'Trial Guest', 'status' => 'booked'] + $guest);
        $this->insert('bookings', [
            'id' => 302, 'guest_name' => 'Canceled Guest', 'status' => 'canceled', 'canceled_at' => '2026-10-23 14:00:00',
        ] + $guest);
        $instructor = new SessionPrincipal(self::id(11), self::id(5), 'instructor');
        $accompanist = new SessionPrincipal(self::id(11), self::id(7), 'instructor');
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $member = new SessionPrincipal(self::id(10), self::id(1), 'member');
        $app = $this->app(self::BEFORE_RELEASE);
        $query = '/teach?from=2026-10-25&to=2026-10-26';
        self::get($app, '/teach', status: 401);
        self::get($app, '/teach', $member, 403);
        self::get($app, '/teach?from=invalid&staffId=invalid', $office, 422);
        self::get($app, '/teach?from=9999-12-31', $office, 422);
        self::get($app, '/teach?from=2026-10-25&to=2026-10-25', $office, 422);
        self::assertSame(405, $app->handle((new ServerRequestFactory())->createServerRequest('HEAD', '/api/v1/teach'))->getStatusCode());
        $default = self::get($app, '/teach', $office);
        self::assertSame('2026-10-18', $default['from']);
        self::assertSame('2026-11-02', $default['to']);

        /** @var list<array<string,mixed>> $own */
        $own = self::get($app, $query, $instructor)['lessons'];
        self::assertSame(self::ids(100, 108, 109), array_column($own, 'id'));
        self::assertSame('instructor', $own[0]['role']);
        self::assertSame(4, $own[0]['active']);
        self::assertSame(1, $own[0]['held']);
        self::assertSame(0, $own[0]['freePlaces']);
        self::assertSame(2, $own[0]['waiting']);
        self::assertSame(1, $own[0]['trials']);
        self::assertSame(1, $own[0]['guests']);
        self::assertSame('2026-10-25T23:00:00Z', $own[0]['dayOverAt']);
        self::assertTrue($own[0]['attendanceOpen']);
        self::assertFalse($own[0]['attendanceTaken']);
        self::assertSame('hidden', $own[1]['visibility']);
        self::assertSame('canceled', $own[1]['status']);
        self::assertFalse($own[1]['attendanceOpen']);

        $filter = $query . '&staffId=' . self::id(24);
        self::assertSame([], self::get($app, $filter, $instructor)['lessons']);
        $unnamed = new SessionPrincipal(self::id(11), self::id(4), 'instructor');
        self::assertSame([], self::get($app, $query, $unnamed)['lessons']);
        /** @var list<array<string,mixed>> $filtered */
        $filtered = self::get($app, $filter, $office)['lessons'];
        self::assertSame(self::ids(100), array_column($filtered, 'id'));
        self::assertSame('instructor', $filtered[0]['role']);
        /** @var list<array<string,mixed>> $playing */
        $playing = self::get($app, $query, $accompanist)['lessons'];
        self::assertSame('accompanist', $playing[0]['role']);

        /** @var list<array<string,mixed>> $draft */
        $draft = self::get($app, '/teach?from=2026-11-04&to=2026-11-05', $instructor)['lessons'];
        self::assertSame('draft-workshop', $draft[0]['eventSlug']);
        self::assertSame(4, $draft[0]['capacity']);
        self::assertSame(4, $draft[0]['freePlaces']);

        $this->database->executeStatement("UPDATE bookings SET status = 'attended' WHERE status = 'booked'");
        $late = $this->app('2026-10-25T23:00:00Z');
        /** @var list<array<string,mixed>> $afterMidnight */
        $afterMidnight = self::get($late, $query, $instructor)['lessons'];
        self::assertFalse($afterMidnight[0]['attendanceOpen']);
        self::assertTrue($afterMidnight[0]['attendanceTaken']);
        self::assertSame('2026-10-26T01:00:00Z', $afterMidnight[2]['dayOverAt']);
        self::assertTrue($afterMidnight[2]['attendanceOpen']);
        /** @var list<array<string,mixed>> $officeAfter */
        $officeAfter = self::get($late, $query, $office)['lessons'];
        self::assertTrue($officeAfter[0]['attendanceOpen']);

        $this->database->executeStatement('DELETE FROM settings');
        self::get($app, $query, $office, 404);
    }

    public function testAttendanceSettlesVisitsAndRecordsChangesInOneTransaction(): void
    {
        $this->database->executeStatement("UPDATE staff SET person_id = (SELECT person_id FROM accounts WHERE role = 'office') WHERE kind = 'instructor'");
        $this->insert('accounts', [
            'id' => 12, 'person_id' => 6, 'email' => 'actual-admin@example.test', 'role' => 'admin',
        ]);
        $this->insert('pass_types', [
            'id' => 301, 'name' => 'Fixture Visits', 'kind' => 'visits', 'visits' => 5, 'validity_days' => 0,
            'price' => 0, 'vat_rate_bp' => 0, 'active' => true, 'created_at' => '2026-10-20 10:00:00',
        ]);
        foreach ([303 => [1, 3], 304 => [7, 2], 305 => [6, 5], 306 => [2, 4]] as $pass => [$person, $visits]) {
            $this->insert('passes', [
                'id' => $pass, 'pass_type_id' => 301, 'person_id' => $person, 'kind' => 'visits',
                'visits_total' => 5, 'visits_remaining' => $visits, 'valid_from' => '2026-10-20', 'valid_until' => '2026-10-25',
                'status' => 'active', 'origin' => 'assignment', 'sold_at' => '2026-10-20 10:00:00', 'price_paid' => 0,
            ]);
        }
        $booked = ['lesson_id' => 100, 'status' => 'booked', 'trial' => false, 'booked_at' => '2026-10-23 09:00:00'];
        $this->insert('bookings', ['id' => 307, 'person_id' => 7] + $booked);
        $this->insert('bookings', ['id' => 308, 'person_id' => 6, 'trial' => true, 'trial_note' => 'Fixture trial'] + $booked);
        $this->insert('bookings', ['id' => 309, 'person_id' => 2, 'source' => 'member'] + $booked);
        $this->insert('bookings', ['id' => 310, 'guest_name' => 'Walk-in'] + $booked);

        $app = $this->app(self::BEFORE_RELEASE);
        $set = static fn(int $id): string => '/bookings/' . self::id($id) . '/attendance';
        $all = '/lessons/' . self::id(100) . '/attendance/all';
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office', impersonatorId: self::id(12));
        $instructor = new SessionPrincipal(self::id(11), self::id(5), 'instructor');
        $unassigned = new SessionPrincipal(self::id(10), self::id(1), 'instructor');
        $invalidActor = new SessionPrincipal(self::id(11), self::id(5), 'office', impersonatorId: self::id(999));
        $attended = ['status' => 'attended'];
        $remaining = function (): array {
            /** @var list<int|numeric-string> $visits */
            $visits = $this->database->fetchFirstColumn('SELECT visits_remaining FROM passes ORDER BY id');

            return array_map(intval(...), $visits);
        };
        self::post($app, $set(201), null, 401, $attended);
        self::post($app, $all, new SessionPrincipal(self::id(10), self::id(1), 'member'), 403);
        self::post($app, $set(201), $unassigned, 404, $attended);
        self::post($app, $set(999), $office, 404, $attended);
        self::post($app, '/bookings/invalid/attendance', $office, 422, $attended);
        self::post($app, $set(201), $office, 400);
        self::post($app, $set(201), $office, 422, ['status' => 'unknown']);
        self::assertSame('already-canceled', self::post($app, $set(203), $office, 409, $attended)['reason']);
        $closed = self::post($app, '/lessons/' . self::id(103) . '/attendance/all', $office, 409);
        self::assertSame('closed', $closed['reason']);
        self::assertSame('canceled', $closed['why']);
        $early = $this->app('2026-10-25T09:00:00Z');
        self::assertSame('not-started', self::post($early, $all, $office, 422)['reason']);

        try {
            self::post($app, $all, $invalidActor, 200);
            self::fail('Expected the booking event insert to fail');
        } catch (ForeignKeyConstraintViolationException) {
            self::assertSame([3, 2, 5, 4], $remaining());
            self::assertCount(5, $this->database->fetchFirstColumn("SELECT 1 FROM bookings WHERE status = 'booked'"));
            self::assertSame([], $this->database->fetchFirstColumn('SELECT 1 FROM pass_redemptions'));
            self::assertSame([], $this->database->fetchFirstColumn('SELECT 1 FROM booking_events'));
        }

        $this->database->executeStatement("UPDATE bookings SET guest_email = 'invalid' WHERE guest_name = 'Walk-in'");
        $invalid = self::post($app, $all, $office, 422);
        $errors = $invalid['errors'] ?? null;
        self::assertIsArray($errors);
        self::assertIsArray($errors[0] ?? null);
        self::assertSame('body.guest.email', $errors[0]['location'] ?? null);
        self::assertCount(5, $this->database->fetchFirstColumn("SELECT 1 FROM bookings WHERE status = 'booked'"));
        self::assertSame([], $this->database->fetchFirstColumn('SELECT 1 FROM booking_events'));
        $this->database->executeStatement("UPDATE bookings SET guest_email = NULL WHERE guest_name = 'Walk-in'");

        /** @var list<array<string,mixed>> $items */
        $items = self::post($app, $all, $office, 200)['items'];
        self::assertSame(self::ids(201, 307, 308, 309, 310), array_column($items, 'id'));
        self::assertSame('Fixture trial', $items[2]['trialNote']);
        self::assertSame('member', $items[3]['source']);
        self::assertSame('Walk-in', $items[4]['guestName']);
        self::assertArrayNotHasKey('personId', $items[4]);
        self::assertArrayNotHasKey('guestEmail', $items[4]);
        self::assertSame([2, 1, 5, 4], $remaining());
        self::assertCount(2, $this->database->fetchFirstColumn('SELECT 1 FROM pass_redemptions'));
        self::assertSame(array_fill(0, 5, 'actual-admin@example.test'), $this->database->fetchFirstColumn(
            'SELECT a.email FROM booking_events e JOIN accounts a ON a.id = e.actor_account_id',
        ));
        self::assertSame(['items' => []], self::post($app, $all, $office, 200));

        $missed = ['status' => 'no_show'];
        self::assertSame('no_show', self::post($app, $set(201), $instructor, 200, $missed)['status']);
        self::post($app, $set(201), $instructor, 200, $missed);
        self::assertCount(6, $this->database->fetchFirstColumn('SELECT 1 FROM booking_events'));
        self::assertSame([2, 1, 5, 4], $remaining());
        $reset = ['status' => 'booked'];
        self::post($app, $set(201), $instructor, 200, $reset);
        self::assertSame([3, 1, 5, 4], $remaining());

        $after = $this->app('2026-10-26T09:00:00Z');
        self::assertSame('day-over', self::post($after, $set(201), $instructor, 422, $attended)['reason']);
        self::post($after, $set(201), $office, 200, $attended);
        self::assertSame([2, 1, 5, 4], $remaining());
        $this->database->executeStatement("UPDATE settings SET redemption_mode = 'booking'");
        self::post($after, $set(201), $office, 200, $reset);
        self::assertSame([2, 1, 5, 4], $remaining());
        self::assertSame([], $this->database->fetchFirstColumn('SELECT 1 FROM outbox_mails'));
    }

    public function testAttendanceSelectsOnlyPassesLockedBeforeSettlement(): void
    {
        $this->insert('pass_types', [
            'id' => 301, 'name' => 'Single Visit', 'kind' => 'visits', 'visits' => 1, 'validity_days' => 0,
            'price' => 0, 'vat_rate_bp' => 0, 'active' => true, 'created_at' => '2026-10-20 10:00:00',
        ]);
        // The statement logger runs between the pass lock and the redemption lookup.
        $sale = function (): void {
            $this->insert('passes', [
                'id' => 302, 'pass_type_id' => 301, 'person_id' => 1, 'kind' => 'visits',
                'visits_total' => 1, 'visits_remaining' => 1, 'valid_from' => '2026-10-25',
                'status' => 'active', 'origin' => 'sale', 'sold_at' => '2026-10-25 12:00:00', 'price_paid' => 0,
            ]);
        };
        $logger = new class ($sale) extends AbstractLogger {
            public bool $inserted = false;

            /** @param Closure():void $insertPass */
            public function __construct(private readonly Closure $insertPass) {}

            /** @param array<string,mixed> $context */
            public function log(mixed $level, string|Stringable $message, array $context = []): void
            {
                $sql = $context['sql'] ?? null;
                if (!$this->inserted && is_string($sql) && str_contains($sql, 'SELECT pass_id, booking_id FROM pass_redemptions')) {
                    ($this->insertPass)();
                    $this->inserted = true;
                }
            }
        };
        $config = new Configuration();
        $config->setMiddlewares([new Middleware($logger)]);
        $writer = DriverManager::getConnection($this->database->getParams(), $config);
        $clock = self::createStub(ClockInterface::class);
        $clock->method('now')->willReturn(new DateTimeImmutable(self::BEFORE_RELEASE));
        $attendance = new Attendance(new AttendanceStore(
            $writer,
            new EventStore($writer),
            new LessonWriteStore($writer),
            new ParticipantReadStore($writer),
        ), $clock);
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        try {
            self::assertSame('attended', $attendance->set($office, self::id(201), 'attended')['status']);
            self::assertTrue($logger->inserted);
            self::assertSame([], $this->database->fetchFirstColumn('SELECT 1 FROM pass_redemptions'));
            self::assertSame('booked', $attendance->set($office, self::id(201), 'booked')['status']);
            self::assertSame('attended', $attendance->set($office, self::id(201), 'attended')['status']);
            self::assertCount(1, $this->database->fetchFirstColumn('SELECT 1 FROM pass_redemptions'));
            self::assertContains($this->database->fetchOne('SELECT visits_remaining FROM passes'), [0, '0']);
        } finally {
            $writer->close();
        }
    }

    private function eventExpiryJobs(DateTimeImmutable $now): BackgroundJobs
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        self::assertIsString($dsn);
        $clock = new class ($now) implements ClockInterface {
            public function __construct(private readonly DateTimeImmutable $instant) {}

            public function now(): DateTimeImmutable
            {
                return $this->instant;
            }
        };

        return new BackgroundJobs($this->database, $dsn, $clock, new NullLogger());
    }

    public function testReleaseVisibilityFiltersAndAvailabilityThroughPublishedRoutes(): void
    {
        $app = $this->app(self::BEFORE_RELEASE);
        $guest = self::get($app, self::RANGE);
        self::assertSame('Europe/Berlin', $guest['timezone']);
        self::assertSame(self::BEFORE_RELEASE, $guest['now']);
        self::assertSame('2026-10-25T23:00:00Z', $guest['releasedUntil']);
        self::assertSame(self::AT_RELEASE, $guest['nextReleaseAt']);
        self::assertSame($guest['releasedUntil'], $guest['visibleUntil']);
        self::assertSame(self::ids(100, 103, 102, 106), self::lessonIds($guest));

        $member = new SessionPrincipal(self::id(10), self::id(1), 'member', null, null);
        $memberRange = self::get($app, self::RANGE, $member);
        self::assertSame('2026-10-27T23:00:00Z', $memberRange['visibleUntil']);
        self::assertSame(self::ids(100, 104, 103, 102, 106), self::lessonIds($memberRange));

        $early = new SessionPrincipal(self::id(10), self::id(1), 'member', 7, self::id(11));
        self::assertSame(self::ids(100, 104, 101, 103, 102, 106), self::lessonIds(self::get($app, self::RANGE, $early)));

        foreach (['instructor', 'office', 'admin'] as $staffRole) {
            $staff = new SessionPrincipal(self::id(11), self::id(5), $staffRole, null, null);
            $staffRange = self::get($app, self::RANGE, $staff);
            self::assertArrayNotHasKey('visibleUntil', $staffRange);
            self::assertSame(self::ids(100, 104, 101, 105, 103, 102, 106, 107), self::lessonIds($staffRange));

            $canceled = self::get($app, '/lessons/' . self::id(103), $staff);
            self::assertSame('canceled', $canceled['status']);
            self::assertArrayNotHasKey('lateCancelFrom', $canceled);
            if ($staffRole === 'instructor') {
                self::assertArrayNotHasKey('admin', $canceled);
            } else {
                self::assertSame([
                    'detached' => true,
                    'activeBookings' => 0,
                    'cancelNote' => 'Hall unavailable',
                    'canceledAt' => '2026-10-25T13:00:00Z',
                    'canceledBy' => ['id' => self::id(11), 'name' => 'Iona Staff'],
                ], $canceled['admin']);
                $event = self::get($app, '/lessons/' . self::id(106), $staff);
                self::assertIsArray($event['admin']);
                self::assertSame(0, $event['admin']['activeBookings']);
            }
        }

        $missing = self::get($app, '/lessons/' . self::id(999), status: 404);
        foreach ([101, 104, 105, 107] as $hidden) {
            self::assertSame($missing, self::get($app, '/lessons/' . self::id($hidden), status: 404));
        }

        $public = self::get($app, '/lessons/' . strtoupper(self::id(100)));
        self::assertSame(0, $public['freePlaces']);
        self::assertSame('2026-10-25T09:59:59.999999Z', $public['bookingClosesAt']);
        self::assertSame('2026-10-25T08:00:00.5Z', $public['lateCancelFrom']);
        self::assertSame(['id' => self::id(20), 'name' => 'Fictional Hall'], $public['location']);
        self::assertSame(['id' => self::id(21), 'name' => 'Studio A', 'position' => 2], $public['room']);
        self::assertSame('Fictional Road 1', $public['locationAddress']);
        self::assertSame('Dance description', $public['courseTypeDescription']);
        self::assertSame(['id' => self::id(31), 'name' => 'First', 'code' => 'F'], $public['levelFrom']);
        self::assertSame(['id' => self::id(32), 'name' => 'Last'], $public['levelTo']);
        self::assertSame(2, $public['stage']);
        self::assertSame(72, $public['ageFromMonths']);
        self::assertSame(144, $public['ageToMonths']);
        self::assertArrayNotHasKey('bookedFor', $public);
        self::assertArrayNotHasKey('waitlist', $public);
        self::assertArrayNotHasKey('admin', $public);

        $personal = self::get($app, '/lessons/' . self::id(100), $member);
        self::assertSame([
            ['personId' => self::id(1), 'name' => 'Mara Steps', 'bookingId' => self::id(201)],
            ['personId' => self::id(1), 'name' => 'Mara Steps', 'bookingId' => self::id(204)],
            ['personId' => self::id(3), 'name' => 'Zoe Alpha', 'bookingId' => self::id(202)],
        ], $personal['bookedFor']);
        self::assertSame([
            ['personId' => self::id(2), 'name' => 'Ari Zebra', 'entryId' => self::id(211), 'status' => 'waiting', 'stayOnList' => true, 'position' => 2],
            ['personId' => self::id(6), 'name' => 'Nia Zebra', 'entryId' => self::id(214), 'status' => 'expired', 'stayOnList' => false, 'expiresAt' => self::BEFORE_RELEASE],
        ], $personal['waitlist']);

        $event = self::get($app, '/lessons/' . self::id(106));
        self::assertSame(self::id(120), $event['eventId']);
        self::assertSame('winter-workshop', $event['eventSlug']);
        self::assertSame(7, $event['capacity']);
        self::assertSame(5, $event['freePlaces']);

        $instructor = new SessionPrincipal(self::id(11), self::id(5), 'instructor', null, null);
        $rental = self::get($app, '/lessons/' . self::id(105), $instructor);
        self::assertSame('rental', $rental['kind']);
        self::assertSame('Example Club', $rental['renter']);
        self::assertSame(0, $rental['freePlaces']);
        self::assertArrayNotHasKey('courseType', $rental);
        self::assertArrayNotHasKey('instructor', $rental);

        self::assertSame(self::ids(100, 103), self::lessonIds(self::get($app, self::RANGE . '&level=' . self::id(33))));
        self::assertSame([], self::lessonIds(self::get($app, self::RANGE . '&level=' . self::id(999))));
        self::assertSame([], self::lessonIds(self::get($app, self::RANGE . '&location=' . self::id(999))));
        self::assertSame(self::ids(100, 103, 102, 106), self::lessonIds(self::get($app, self::RANGE . '&location=' . self::id(20))));
        self::assertSame(self::ids(100), self::lessonIds(self::get(
            $app,
            self::RANGE . '&instructor=' . self::id(24) . '&courseType=' . self::id(22) . '&level=' . self::id(31),
        )));
        self::assertSame([], self::lessonIds(self::get($app, '/schedule?from=2026-11-04&to=2026-11-05')));
        self::assertSame([], self::lessonIds(self::get($app, '/schedule?from=0001-01-01&to=0001-01-02')));
        self::assertSame(self::ids(100), self::lessonIds(self::get($app, '/schedule?from=2026-10-25&to=2026-10-26&to=unused')));

        $released = self::get($this->app(self::AT_RELEASE), self::RANGE);
        self::assertSame('2026-11-01T23:00:00Z', $released['releasedUntil']);
        self::assertSame('2026-11-01T17:00:00Z', $released['nextReleaseAt']);
        self::assertSame(self::ids(100, 101, 103, 102, 106), self::lessonIds($released));

        if ($this->postgres) {
            // PostgreSQL counts every month as 30 days when comparing intervals;
            // the Go reader uses the same rule, including year components.
            $this->database->executeStatement("UPDATE settings SET cancellation_cutoff = '1 year -350 days'");
            $longCutoff = self::get($app, '/lessons/' . self::id(100));
            self::assertSame('2026-10-15T10:00:00.5Z', $longCutoff['lateCancelFrom']);
        }

        $this->database->executeStatement("UPDATE settings SET released_through = '2026-10-29'");
        $manual = self::get($app, self::RANGE);
        self::assertSame('2026-10-29T23:00:00Z', $manual['releasedUntil']);
        self::assertSame(self::AT_RELEASE, $manual['nextReleaseAt']);
        self::assertSame(self::ids(100, 101, 103, 102, 106), self::lessonIds($manual));

        $this->database->executeStatement('DELETE FROM settings');
        self::assertSame($missing, self::get($app, self::RANGE, status: 404));
        self::assertSame($missing, self::get($app, '/lessons/' . self::id(100), status: 404));
    }

    public function testOfficeStatisticsUseSchoolDatesAndExcludePrivateParticipantNames(): void
    {
        $app = $this->app('2026-10-25T23:30:00Z');
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $member = new SessionPrincipal(self::id(10), self::id(1), 'member', impersonatorId: self::id(11));
        $path = '/admin/statistics';
        self::get($app, $path, status: 401);
        self::get($app, $path . '?range=invalid', $member, status: 403);
        self::get($app, $path . '?range=invalid', $office, status: 422);
        self::get($app, $path . '?range=custom&from=2026-02-30&to=2026-03-02', $office, status: 422);
        self::get($app, $path . '?range=custom&from=2026-11-04', $office, status: 422);
        self::get($app, $path . '?range=custom&from=2026-11-04&to=2026-11-01', $office, status: 422);
        self::get($app, $path . '?range=custom&from=2025-01-01&to=2026-01-02', $office, status: 422);

        $this->database->executeStatement("UPDATE persons SET anonymized_at = '2026-10-24 12:00:00' WHERE given_name = 'Ari'");
        $primary = $this->database->fetchOne("SELECT id FROM persons WHERE given_name = 'Mara'");
        self::assertIsString($primary);
        $this->database->executeStatement("UPDATE persons SET merged_into = ? WHERE given_name = 'Other'", [$primary], [$this->postgres ? ParameterType::STRING : ParameterType::BINARY]);
        $this->database->executeStatement("UPDATE bookings SET late = TRUE WHERE status = 'canceled'");
        $common = ['booked_at' => '2026-10-22 14:00:00', 'status' => 'booked'];
        $this->insert('bookings', ['id' => 205, 'lesson_id' => 104, 'person_id' => 2, 'trial' => false] + $common);
        $this->insert('bookings', ['id' => 206, 'lesson_id' => 101, 'person_id' => 4, 'trial' => false] + $common);
        $this->insert('bookings', ['id' => 207, 'lesson_id' => 101, 'guest_name' => 'Synthetic guest', 'trial' => true] + $common);
        $this->insert('waitlist_entries', [
            'id' => 215, 'lesson_id' => 100, 'person_id' => 3, 'status' => 'withdrawn',
            'joined_at' => '2026-10-22 14:00:00', 'stay_on_list' => false,
        ]);

        $this->seedStatisticsSales();
        $preset = self::get($app, $path, $office);
        self::assertSame(['last4Weeks', '2026-10-05', '2026-10-26', 'week'], [$preset['range'], $preset['from'], $preset['to'], $preset['granularity']]);
        self::assertSame('2026-08-10', self::get($app, $path . '?range=last12Weeks', $office)['from']);
        $this->insert('terms', ['id' => 34, 'name' => 'New term', 'starts_on' => '2026-10-20', 'ends_on' => '2026-11-30']);
        $term = self::get($app, $path . '?range=term', $office);
        self::assertSame(['id' => self::id(34), 'name' => 'New term'], $term['term']);
        self::assertSame('2026-10-20', $term['from']);
        self::assertSame('no term includes today', self::get($this->app('2027-01-01T12:00:00Z'), $path . '?range=term', $office, status: 404)['detail']);
        $wide = self::get($app, $path . '?range=custom&from=2026-01-05&to=2026-07-06', $office);
        self::assertSame('month', $wide['granularity']);
        self::assertIsArray($wide['periods']);
        self::assertCount(7, $wide['periods']);
        self::assertSame(['2026-01-05', '2026-02-01', '2026-03-01', '2026-04-01', '2026-05-01', '2026-06-01', '2026-07-01'], array_column($wide['periods'], 'start'));

        $report = self::get($app, $path . '?range=custom&from=2026-10-25&to=2026-11-04', $office);
        self::assertSame(['Europe/Berlin', 'EUR', '2026-10-26', 'week'], [$report['timezone'], $report['currency'], $report['today'], $report['granularity']]);
        self::assertIsArray($report['totals']);
        self::assertEquals([
            'lessons' => 4, 'capacity' => 16, 'bookings' => 6, 'utilization' => 0.375,
            'attended' => 1, 'noShows' => 1, 'noShowRate' => 0.5, 'lateCancellations' => 1,
            'trials' => 1, 'guests' => 1, 'waitlistEntries' => 5, 'canceledLessons' => 1,
            'activeMembers' => 4, 'passesSold' => 0, 'salesGross' => 0, 'eventFees' => 2500,
        ], $report['totals']);
        self::assertIsArray($report['periods']);
        self::assertSame(['2026-10-25', '2026-10-26', '2026-11-02'], array_column($report['periods'], 'start'));
        self::assertSame(['2026-10-25', '2026-11-01', '2026-11-04'], array_column($report['periods'], 'end'));
        self::assertSame([1, -1, 0], array_column($report['periods'], 'passesSold'));
        self::assertSame([8000, -8000, 0], array_column($report['periods'], 'salesGross'));
        self::assertIsArray($report['periods'][2]);
        self::assertArrayNotHasKey('noShowRate', $report['periods'][2]);
        self::assertIsArray($report['classes']);
        self::assertSame([self::id(40)], array_column($report['classes'], 'classId'));
        self::assertSame(self::ids(104, 101, 102), array_column($report['classes'], 'lessonId'));
        self::assertIsArray($report['classes'][0]);
        self::assertSame(['id' => self::id(31), 'name' => 'First', 'code' => 'F'], $report['classes'][0]['levelFrom']);
        self::assertSame(['id' => self::id(32), 'name' => 'Last'], $report['classes'][0]['levelTo']);
        self::assertSame(0, $report['classes'][0]['weekday']);
        self::assertSame('11:00', $report['classes'][0]['startTime']);
        self::assertIsArray($report['classes'][1]);
        self::assertSame('2026-10-27T10:00:00.5Z', $report['classes'][1]['startsAt']);
        foreach (['rooms' => 'roomId', 'courseTypes' => 'courseTypeId', 'instructors' => 'staffId'] as $group => $idKey) {
            self::assertIsArray($report[$group]);
            self::assertCount(1, $report[$group]);
            self::assertIsArray($report[$group][0]);
            self::assertArrayHasKey($idKey, $report[$group][0]);
            self::assertSame(6, $report[$group][0]['bookings']);
        }
        self::assertIsArray($report['topParticipants']);
        self::assertSame(self::ids(1, 3), array_column($report['topParticipants'], 'personId'));
        self::assertSame([2, 1], array_column($report['topParticipants'], 'bookings'));
        self::assertEquals([
            ['eventId' => self::id(120), 'title' => 'Workshop', 'registered' => 2, 'revenue' => 2500],
            ['eventId' => self::id(121), 'title' => 'Workshop', 'registered' => 0, 'revenue' => 0],
        ], $report['events']);
        $this->database->executeStatement('DELETE FROM settings');
        self::assertSame('resource not found', self::get($app, $path, $office, status: 404)['detail']);
    }

    public function testStatisticsIncludeTheLastRepresentableSchoolDate(): void
    {
        $this->database->executeStatement("UPDATE settings SET timezone = 'America/New_York'");
        $this->insertLesson(108, '9999-12-31', 'regular', 'public', [
            'starts_at' => '9999-12-31 23:00:00.500000', 'ends_at' => '9999-12-31 23:45:00.500000',
        ]);
        $office = new SessionPrincipal(self::id(11), self::id(5), 'office');
        $report = self::get($this->app(self::AT_RELEASE), '/admin/statistics?range=custom&from=9999-12-31&to=9999-12-31', $office);
        self::assertIsArray($report['totals']);
        self::assertSame(1, $report['totals']['lessons']);
        self::assertIsArray($report['periods']);
        self::assertSame(['9999-12-31'], array_column($report['periods'], 'end'));
    }

    private function seedStatisticsSales(): void
    {
        foreach ([
            400 => ['2026-10-25', 8000, null],
            401 => ['2026-10-26', -8000, 400],
            402 => ['2026-10-27', 2500, null],
            403 => ['2026-10-27', 1500, null],
            404 => ['2026-10-28', -1500, 403],
        ] as $id => [$date, $gross, $cancels]) {
            $this->insert('receipts', [
                'id' => $id, 'number' => "R-2026-$id", 'year' => 2026, 'sequence' => $id,
                'kind' => $cancels === null ? 'sale' : 'cancellation', 'cancels_receipt_id' => $cancels,
                'issued_at' => "$date 10:00:00", 'issue_date' => $date, 'locale' => 'en',
                'issuer_name' => 'Fictional Hall', 'issuer_address' => 'Fixture Street', 'issuer_tax_id' => '1234',
                'tax_mode' => 'exempt', 'recipient_name' => 'Fixture recipient', 'payment_method' => 'cash',
                'currency' => 'EUR', 'net' => $gross, 'vat' => 0, 'gross' => $gross,
            ]);
        }
        $this->insert('event_registrations', [
            'id' => 224, 'event_id' => 120, 'person_id' => 6, 'status' => 'registered', 'payment' => 'paid',
            'price' => 2500, 'vat_rate_bp' => 0, 'receipt_id' => 402,
            'created_at' => '2026-10-27 10:00:00', 'updated_at' => '2026-10-27 10:00:00',
        ]);
        $this->insert('event_registrations', [
            'id' => 225, 'event_id' => 120, 'person_id' => 7, 'status' => 'canceled', 'payment' => 'refunded',
            'price' => 1500, 'vat_rate_bp' => 0, 'receipt_id' => 403, 'cancellation_receipt_id' => 404,
            'created_at' => '2026-10-27 10:00:00', 'updated_at' => '2026-10-28 10:00:00', 'canceled_at' => '2026-10-28 10:00:00',
        ]);
    }

    private function seedSchedule(): void
    {
        $durations = $this->postgres ? "'0.500001 seconds', '2 hours'" : '500001, 7200000000';
        $this->database->executeStatement(
            'INSERT INTO settings (school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time, '
            . 'release_weeks_ahead, default_visible_days, booking_cutoff, cancellation_cutoff) '
            . "VALUES ('Fictional Hall', 'Europe/Berlin', 'en', 'EUR', 'R', 0, '18:00:00', 1, 2, $durations)",
        );
        foreach ([
            1 => ['Mara', 'Steps'], 2 => ['Ari', 'Zebra'], 3 => ['Zoe', 'Alpha'],
            4 => ['Other', 'Person'], 5 => ['Iona', 'Staff'], 6 => ['Nia', 'Zebra'], 7 => ['Third', 'Person'],
        ] as $id => [$given, $family]) {
            $this->insert('persons', ['id' => $id, 'given_name' => $given, 'family_name' => $family]);
        }
        $this->insert('accounts', ['id' => 10, 'person_id' => 1, 'email' => 'member@example.test', 'role' => 'member']);
        $this->insert('accounts', ['id' => 11, 'person_id' => 5, 'email' => 'office@example.test', 'role' => 'office']);
        foreach ([2, 3, 6] as $child) {
            $this->insert('person_guardians', ['guardian_id' => 1, 'dependent_id' => $child]);
        }

        $this->insert('locations', ['id' => 20, 'name' => 'Fictional Hall', 'address' => 'Fictional Road 1']);
        $this->insert('rooms', ['id' => 21, 'location_id' => 20, 'name' => 'Studio A', 'capacity' => 18, 'position' => 2]);
        $this->insert('course_types', ['id' => 22, 'name' => 'Ballet', 'description' => 'Dance description']);
        $this->insert('staff', ['id' => 23, 'display_name' => 'Studio Instructor', 'kind' => 'instructor', 'active' => true]);
        $this->insert('staff', ['id' => 24, 'display_name' => 'Studio Pianist', 'kind' => 'accompanist', 'active' => true]);
        $this->insert('terms', ['id' => 30, 'name' => 'Autumn Term', 'starts_on' => '2026-10-01', 'ends_on' => '2026-11-30']);
        $this->insert('levels', ['id' => 31, 'name' => 'First', 'code' => 'F', 'position' => 1]);
        $this->insert('levels', ['id' => 32, 'name' => 'Last', 'position' => 3]);
        $this->insert('levels', ['id' => 33, 'name' => 'Middle', 'code' => 'M', 'position' => 2]);
        $duration = $this->postgres ? '1 hour' : 3600000000;
        $class = [
            'term_id' => 30, 'course_type_id' => 22, 'room_id' => 21, 'instructor_id' => 23,
            'weekday' => 0, 'start_time' => '11:00:00', 'duration' => $duration, 'capacity' => 4,
        ];
        $this->insert('classes', [
            'id' => 40, 'level_from_id' => 32, 'level_to_id' => 31, 'stage' => 2, 'age_from_months' => 72, 'age_to_months' => 144,
        ] + $class);
        $this->insert('classes', ['id' => 41, 'all_levels' => true] + $class);

        foreach ([
            [100, '2026-10-25', 'regular', 'public', ['class_id' => 40, 'slot_date' => '2026-10-25', 'accompanist_id' => 24]],
            [101, '2026-10-28', 'regular', 'public', []],
            [102, '2026-10-30', 'workshop', 'public', []],
            [103, '2026-10-29', 'private', 'public', [
                'class_id' => 41, 'slot_date' => '2026-10-29', 'status' => 'canceled', 'detached' => true,
                'cancel_note' => 'Hall unavailable', 'canceled_at' => '2026-10-25 13:00:00', 'canceled_by' => 11,
            ]],
            [104, '2026-10-27', 'regular', 'members', []],
            [105, '2026-10-28', 'rental', 'hidden', ['course_type_id' => null, 'instructor_id' => null, 'capacity' => 0, 'renter' => 'Example Club']],
        ] as [$id, $date, $kind, $visibility, $fields]) {
            $this->insertLesson($id, $date, $kind, $visibility, $fields);
        }
        foreach ([120 => 'published', 121 => 'draft'] as $id => $status) {
            $this->insert('events', [
                'id' => $id, 'slug' => $status === 'draft' ? 'draft-workshop' : 'winter-workshop', 'title' => 'Workshop',
                'course_type_id' => 22, 'capacity' => 7, 'status' => $status, 'price' => 0, 'vat_rate_bp' => 0,
                'created_at' => '2026-10-24 10:00:00', 'updated_at' => '2026-10-24 10:00:00',
            ]);
        }
        $this->insertLesson(106, '2026-11-03', 'workshop', 'public', ['event_id' => 120]);
        $this->insertLesson(107, '2026-11-04', 'workshop', 'public', ['event_id' => 121]);

        foreach ([
            201 => [1, 'booked', null], 202 => [3, 'attended', null],
            203 => [4, 'canceled', null], 204 => [1, 'no_show', 4],
        ] as $id => [$person, $status, $mergedFrom]) {
            $this->insert('bookings', [
                'id' => $id, 'lesson_id' => 100, 'person_id' => $person, 'status' => $status, 'trial' => false,
                'booked_at' => '2026-10-22 14:00:00', 'canceled_at' => $status === 'canceled' ? '2026-10-23 14:00:00' : null,
                'merged_from' => $mergedFrom,
            ]);
        }
        foreach ([210 => [4, null], 211 => [2, null], 213 => [7, '2026-10-26 17:00:00'], 214 => [6, '2026-10-25 16:59:59']] as $id => [$person, $expires]) {
            $this->insert('waitlist_entries', [
                'id' => $id, 'lesson_id' => 100, 'person_id' => $person, 'status' => $expires === null ? 'waiting' : 'invited',
                'stay_on_list' => $id === 211, 'joined_at' => '2026-10-22 14:00:00',
                'invited_at' => $expires === null ? null : '2026-10-23 14:00:00', 'expires_at' => $expires,
                'invite_token_hash' => $expires === null ? null : hash('sha256', (string) $id, true),
            ]);
        }
        foreach ([221 => [1, 'registered', null], 222 => [2, 'invited', '2026-10-26 17:00:00'], 223 => [3, 'invited', '2026-10-25 16:59:59']] as $id => [$person, $status, $expires]) {
            $this->insert('event_registrations', [
                'id' => $id, 'event_id' => 120, 'person_id' => $person, 'status' => $status, 'payment' => 'unpaid',
                'price' => 0, 'vat_rate_bp' => 0, 'created_at' => '2026-10-22 14:00:00', 'updated_at' => '2026-10-23 14:00:00',
                'invited_at' => $expires === null ? null : '2026-10-23 14:00:00', 'expires_at' => $expires,
                'invite_token_hash' => $expires === null ? null : hash('sha256', (string) $id, true),
            ]);
        }
    }

    /** @param array<string, string|int|bool|null> $fields */
    private function insertLesson(int $id, string $date, string $kind, string $visibility, array $fields): void
    {
        $this->insert('lessons', $fields + [
            'id' => $id, 'room_id' => 21, 'course_type_id' => 22, 'instructor_id' => 23,
            'starts_at' => "$date 10:00:00.500000", 'ends_at' => "$date 11:00:00.500000",
            'kind' => $kind, 'visibility' => $visibility, 'status' => 'scheduled', 'capacity' => 4,
        ]);
    }

    /** @param array<string, string|int|bool|null> $fields */
    private function insert(string $table, array $fields): void
    {
        $params = [];
        $types = [];
        foreach ($fields as $name => $value) {
            $type = match (true) {
                $value === null => ParameterType::NULL,
                is_bool($value) => ParameterType::BOOLEAN,
                is_int($value) => ParameterType::INTEGER,
                default => ParameterType::STRING,
            };
            if (is_int($value) && ($name === 'id' || str_ends_with($name, '_id') || in_array($name, ['canceled_by', 'merged_from'], true))) {
                $text = self::id($value);
                $value = $this->postgres ? $text : pack('H*', str_replace('-', '', $text));
                $type = $this->postgres ? ParameterType::STRING : ParameterType::BINARY;
            } elseif ($value !== null && in_array($name, ['invite_token_hash', 'image'], true)) {
                if ($this->postgres) {
                    assert(is_string($value));
                    $value = '\x' . bin2hex($value);
                } else {
                    $type = ParameterType::BINARY;
                }
            }
            $params[$name] = $value;
            $types[$name] = $type;
        }
        $this->database->insert($table, $params, $types);
    }

    /**
     * @param (Closure(ReceiptDocument,string):string)|null $renderReceipt
     * @param (Closure():void)|null $wakeLessonMail
     * @return App<ContainerInterface|null>
     */
    private function app(string $now, ?Closure $renderReceipt = null, ?Closure $wakeLessonMail = null): App
    {
        $app = AppFactory::create();
        $clock = new class ($now) implements ClockInterface {
            public function __construct(private string $now) {}

            public function now(): DateTimeImmutable
            {
                return new DateTimeImmutable($this->now);
            }
        };
        $scheduleStore = new ScheduleStore($this->database);
        ScheduleRoutes::register($app, $scheduleStore, $clock);
        (new TeachingScheduleRoutes(new TeachingSchedule($scheduleStore, $clock)))->register($app);
        (new LessonSignupRoutes(new LessonSignup(
            $scheduleStore,
            new ParticipantReadStore($this->database),
            new FamilyStore($this->database),
            $clock,
        )))->register($app);
        $limits = new RateLimitStore($this->database, $clock);
        $proxies = TrustedProxies::parse('');
        $app->get('/api/v1/waitlist-invites/{token}', new WaitlistInviteEndpoint(
            (new WaitlistInvites(new WaitlistInviteStore($this->database, $scheduleStore), $clock))->view(...),
            $proxies,
            $limits,
        ));
        $timetable = new TimetableStore($this->database);
        $lessons = new OneOffLesson($timetable, $clock);
        CatalogAdminRoutes::registerLessonCreation($app, $lessons);
        $app->get('/api/v1/admin/statistics', new StatisticsEndpoint(new StatisticsService(new StatisticsStore($this->database), $scheduleStore, $clock)));
        $eventStore = new EventStore($this->database);
        $registrationStore = new EventRegistrationStore($this->database);
        $registrations = new EventRegistrationReads($eventStore, $registrationStore, $clock);
        $app->get('/api/v1/event-invites/{token}', new WaitlistInviteEndpoint($registrations->invite(...), $proxies, $limits));
        $recipients = new EventWaitlistStore($this->database);
        $lessonWrites = new LessonWriteStore($this->database);
        (new AttendanceRoutes(new Attendance(
            new AttendanceStore($this->database, $eventStore, $lessonWrites, new ParticipantReadStore($this->database)),
            $clock,
        )))->register($app);
        $lessonParticipants = new LessonRecipients($lessonWrites, $registrationStore, new ScheduleRecipients($recipients));
        (new LessonNoticeRoutes(new LessonNotices(
            new LessonNoticeStore($this->database, $eventStore, $lessonWrites, $scheduleStore),
            $recipients,
            $lessonParticipants,
            new Renderer(),
            $clock,
            $wakeLessonMail,
        )))->register($app);
        $lessonOffers = new LessonWaitlistOffers(
            $scheduleStore,
            $lessonWrites,
            $recipients,
            new Renderer(),
            static fn(): string => 'https://school.example.test/dance/schedule',
            static fn(): bool => true,
        );
        (new LessonAdminRoutes(new LessonAdministration(
            $eventStore,
            $lessonWrites,
            $scheduleStore,
            $lessonParticipants,
            $recipients,
            $timetable,
            $lessonOffers,
            new Renderer(),
            $clock,
            $wakeLessonMail,
        )))->register($app);
        $waitlist = new EventWaitlist(
            $eventStore,
            $recipients,
            $clock,
            new Renderer(),
            'https://school.example.test/dance/schedule',
            false,
        );
        $eventWaitlist = static fn(): EventWaitlist => $waitlist;
        $receiptWrites = new ReceiptWriteStore($this->database);
        $eventReceipts = new ReceiptIssuance($receiptWrites, new Renderer(), $renderReceipt);
        $passReads = new PassReadStore($this->database, $scheduleStore);
        $passLedger = new PassLedgerStore($this->database);
        (new PassIssueRoutes(
            new PassIssuance(new PassIssueStore($this->database, new FamilyStore($this->database), $passReads, $eventStore), $eventReceipts, $clock),
            new PassVoiding($eventStore, $passLedger, new PassRead($passReads, $clock), $passReads, $clock),
        ))->register($app);
        $receiptReads = new ReceiptReadStore($this->database, $clock);
        (new ReceiptReadRoutes($receiptReads, new ReceiptCancellation(
            $eventStore,
            $registrationStore,
            $passLedger,
            $receiptWrites,
            $receiptReads,
            $eventReceipts,
            $clock,
        )))->register($app);
        $eventCancellations = new EventCancellations($eventStore, $registrationStore, $eventReceipts, $clock, $eventWaitlist);
        EventRoutes::register($app, $eventStore, $clock, $lessons, $eventWaitlist, $eventCancellations);
        (new EventRegistrationRoutes(
            $registrations,
            static fn(): EventRegistrationWrites => new EventRegistrationWrites($eventStore, $registrationStore, $waitlist, $clock),
            new EventPayments($eventStore, $registrationStore, $eventReceipts, $clock),
            $eventCancellations,
        ))->register($app);
        $app->post('/api/v1/event-invites/{token}/accept', new WaitlistInviteEndpoint(
            static fn(string $token): ?true => $waitlist->respondByToken($token, accept: true),
            $proxies,
            $limits,
        ));
        $app->post('/api/v1/event-invites/{token}/decline', new WaitlistInviteEndpoint(
            static fn(string $token): ?true => $waitlist->respondByToken($token, accept: false),
            $proxies,
            $limits,
        ));
        $app->addRoutingMiddleware();

        return $app;
    }

    /**
     * @param App<ContainerInterface|null> $app
     * @return array<array-key, mixed>
     */
    private static function get(App $app, string $path, ?SessionPrincipal $principal = null, int $status = 200): array
    {
        $request = (new ServerRequestFactory())->createServerRequest('GET', '/api/v1' . $path)
            ->withAttribute('sessionPrincipal', $principal);
        $response = $app->handle($request);
        $json = (string) $response->getBody();
        self::assertSame($status, $response->getStatusCode(), $json);
        $body = json_decode($json, true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($body);

        return $body;
    }

    /**
     * @param array<array-key, mixed> $schedule
     * @return list<mixed>
     */
    private static function lessonIds(array $schedule): array
    {
        $lessons = $schedule['lessons'];
        self::assertIsArray($lessons);

        return array_column($lessons, 'id');
    }

    private static function id(int $number): string
    {
        // These legacy UUIDs have digit-only bytes in MySQL. PHP would
        // coerce the raw bytes to integers if used as array keys.
        return match ($number) {
            3 => '32333435-3637-3839-3031-323334353637',
            100 => '31323334-3536-3738-3930-313233343536',
            default => sprintf('00000000-0000-7000-8000-%012x', $number),
        };
    }

    /** @return list<string> */
    private static function ids(int ...$numbers): array
    {
        return array_map(self::id(...), array_values($numbers));
    }
}
