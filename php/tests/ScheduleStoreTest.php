<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Slim\App;
use Slim\Factory\AppFactory;
use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\Psr7\Factory\StreamFactory;
use StudioSchedule\Http\CatalogAdminRoutes;
use StudioSchedule\Http\EventRoutes;
use StudioSchedule\Http\ScheduleRoutes;
use StudioSchedule\Http\StatisticsEndpoint;
use StudioSchedule\Http\TrustedProxies;
use StudioSchedule\Http\WaitlistInviteEndpoint;
use StudioSchedule\Service\OneOffLesson;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\StatisticsService;
use StudioSchedule\Service\WaitlistInvites;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\EventStore;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\PublicSettingsStore;
use StudioSchedule\Store\RateLimitStore;
use StudioSchedule\Store\ScheduleStore;
use StudioSchedule\Store\Schema;
use StudioSchedule\Store\StatisticsStore;
use StudioSchedule\Store\TimetableStore;
use StudioSchedule\Store\WaitlistInviteStore;

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

    /** @return App<ContainerInterface|null> */
    private function app(string $now): App
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
        $app->get('/api/v1/waitlist-invites/{token}', new WaitlistInviteEndpoint(
            new WaitlistInvites(new WaitlistInviteStore($this->database, $scheduleStore), $clock),
            TrustedProxies::parse(''),
            new RateLimitStore($this->database, $clock),
        ));
        CatalogAdminRoutes::registerLessonCreation($app, new OneOffLesson(new TimetableStore($this->database), $clock));
        $app->get('/api/v1/admin/statistics', new StatisticsEndpoint(new StatisticsService(new StatisticsStore($this->database), $scheduleStore, $clock)));
        EventRoutes::register($app, new EventStore($this->database), $clock);
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
