<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Configuration;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\DriverManager;
use Doctrine\DBAL\Logging\Middleware;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Log\AbstractLogger;
use Slim\Factory\AppFactory;
use Slim\Psr7\Factory\ResponseFactory;
use Slim\Psr7\Factory\ServerRequestFactory;
use Stringable;
use StudioSchedule\Bootstrap;
use StudioSchedule\Domain\AccentColor;
use StudioSchedule\Domain\BrandingImage;
use StudioSchedule\Http\PublicSettingsEndpoint;
use StudioSchedule\Service\PublicSettings;
use StudioSchedule\Service\SchoolSetup;
use StudioSchedule\Service\SetupInput;
use StudioSchedule\Service\SystemClock;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\PublicSettingsStore;
use StudioSchedule\Store\Schema;
use StudioSchedule\Store\SetupStore;
use Symfony\Component\Uid\Uuid;

final class PublicSettingsTest extends TestCase
{
    private const string PNG = "\x89PNG\r\n\x1a\n\x00\xfflogo";
    private const string JPEG = "\xff\xd8\xff\x00replacement";
    private const string WEBP = "RIFF\x00\x00\x00\x00WEBPVP8 background";

    private static ?string $dsn = null;
    private Connection $db;

    protected function setUp(): void
    {
        $this->db = Database::connect(self::fixtureDsn());
        if (self::$dsn !== null) {
            return;
        }

        self::assertSame([], (new Schema($this->db))->relations(), 'Settings tests require an empty disposable database');
        self::$dsn = self::fixtureDsn();
        $migrations = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform
            ? self::postgresMigrations()
            : __DIR__ . '/../migrations/mysql';
        (new Migrator(self::$dsn))->up($migrations);
    }

    protected function tearDown(): void
    {
        if (!isset($this->db)) {
            return;
        }
        try {
            if (self::$dsn !== null) {
                foreach (['event_registrations', 'events', 'passes', 'pass_types', 'lessons', 'staff', 'rooms', 'locations',
                    'course_types', 'sessions', 'account_tokens', 'accounts', 'persons', 'settings'] as $table) {
                    $this->db->executeStatement('DELETE FROM ' . $table);
                }
            }
        } finally {
            $this->db->close();
        }
    }

    public static function tearDownAfterClass(): void
    {
        if (self::$dsn === null) {
            return;
        }

        $db = Database::connect(self::$dsn);
        $postgres = $db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        try {
            if (!$postgres) {
                $db->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            foreach ($db->createSchemaManager()->listTableNames() as $table) {
                $suffix = $postgres ? ' CASCADE' : '';
                $db->executeStatement('DROP TABLE IF EXISTS ' . $db->quoteSingleIdentifier($table) . $suffix);
            }
            if ($postgres) {
                $db->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
        } finally {
            $db->close();
            self::$dsn = null;
        }
    }

    public function testMissingSettingsAreReportedAsResourceNotFound(): void
    {
        foreach (['public', 'logo', 'background'] as $path) {
            $response = $this->get($path);
            self::assertSame(404, $response->getStatusCode());
            self::assertSame('application/problem+json', $response->getHeaderLine('Content-Type'));
            self::assertSame('resource not found', self::decoded($response)['detail'] ?? null);
        }
    }

    public function testPublicSettingsUseDefaultsForLegacyValues(): void
    {
        $this->initialize();
        $this->db->executeStatement('UPDATE settings SET default_locale = ?', ['fr-CA']);
        $response = $this->get('public');

        self::assertSame(200, $response->getStatusCode());
        self::assertSame('application/json', $response->getHeaderLine('Content-Type'));
        self::assertEquals([
            'schoolName' => 'Example Studio',
            'theme' => 'programme',
            'hasLogo' => false,
            'hasBackground' => false,
            'timezone' => 'Europe/Vienna',
            'defaultLocale' => 'en',
            'currency' => 'EUR',
        ], self::decoded($response));
    }

    public function testPublicSettingsIncludeOnlyPublicBrandingFields(): void
    {
        $this->initialize();
        $notice = "Example Studio\nOffice";
        $this->db->executeStatement(
            "UPDATE settings SET default_locale = ?, legal_notice = ?, accent_color = '#7a1f3d', theme = 'werkstatt', issuer_tax_id = 'internal'",
            ["\u{2003}DE_at\u{00a0}", $notice],
        );
        $this->writeImage('logo', 'image/png', self::PNG);
        $this->writeImage('background', 'image/webp', self::WEBP);

        $response = $this->get('public');
        self::assertSame(200, $response->getStatusCode());
        self::assertEquals([
            'schoolName' => 'Example Studio',
            'legalNotice' => $notice,
            'accentColor' => '#7a1f3d',
            'accentColorDark' => '#ee9cae',
            'theme' => 'werkstatt',
            'hasLogo' => true,
            'hasBackground' => true,
            'timezone' => 'Europe/Vienna',
            'defaultLocale' => 'de',
            'currency' => 'EUR',
        ], self::decoded($response));
    }

    public function testWebManifestReflectsAccentsLanguageAndTheMountPath(): void
    {
        $request = (new ServerRequestFactory())->createServerRequest('GET', '/studio/manifest.webmanifest');
        $missing = $this->bootstrapRequest($request);
        self::assertSame(404, $missing->getStatusCode());
        self::assertSame('resource not found', self::decoded($missing)['detail'] ?? null);

        $this->initialize();
        $response = $this->bootstrapRequest($request);
        self::assertSame(200, $response->getStatusCode());
        self::assertSame('application/manifest+json', $response->getHeaderLine('Content-Type'));
        self::assertSame('public, max-age=300', $response->getHeaderLine('Cache-Control'));
        $body = self::decoded($response);
        self::assertEquals([
            'id' => '/studio/',
            'name' => 'Example Studio',
            'short_name' => 'Example Studio',
            'description' => 'Class schedule and booking',
            'lang' => 'en',
            'start_url' => '/studio/',
            'scope' => '/studio/',
            'display' => 'standalone',
            'theme_color' => '#7f1f35',
            'background_color' => '#faf7f2',
            'icons' => [
                ['src' => '/studio/icons/icon-192.png', 'sizes' => '192x192', 'type' => 'image/png'],
                ['src' => '/studio/icons/icon-512.png', 'sizes' => '512x512', 'type' => 'image/png'],
                ['src' => '/studio/icons/icon-maskable-512.png', 'sizes' => '512x512', 'type' => 'image/png', 'purpose' => 'maskable'],
            ],
        ], $body);

        $this->db->executeStatement("UPDATE settings SET default_locale = 'de', accent_color = '#7a1f3d'");
        $changed = self::decoded($this->bootstrapRequest($request));
        self::assertSame('de', $changed['lang'] ?? null);
        self::assertSame('Stundenplan und Buchung', $changed['description'] ?? null);
        self::assertSame('#7a1f3d', $changed['theme_color'] ?? null);
    }

    /** @return iterable<string, array{string, string, string}> */
    public static function imageExamples(): iterable
    {
        yield 'logo' => ['logo', 'image/png', self::PNG];
        yield 'background' => ['background', 'image/webp', self::WEBP];
    }

    #[DataProvider('imageExamples')]
    public function testBrandingImagesRevalidateWithoutFetchingTheBytes(string $kind, string $type, string $bytes): void
    {
        $this->initialize();
        $missing = $this->get($kind, '*');
        self::assertSame(404, $missing->getStatusCode());
        self::assertSame('no ' . $kind, self::decoded($missing)['detail'] ?? null);

        $this->writeImage($kind, $type, $bytes);
        $etag = self::etag($type, $bytes);
        $response = $this->get($kind, '"stale"');
        self::assertSame(200, $response->getStatusCode());
        self::assertSame($bytes, (string) $response->getBody());
        self::assertSame($type, $response->getHeaderLine('Content-Type'));
        self::assertSame($etag, $response->getHeaderLine('ETag'));
        self::assertSame('public, no-cache', $response->getHeaderLine('Cache-Control'));
        self::assertSame('inline', $response->getHeaderLine('Content-Disposition'));

        $logger = new class extends AbstractLogger {
            /** @var list<string> */
            private array $queries = [];

            /** @param array<string, mixed> $context */
            public function log(mixed $level, string|Stringable $message, array $context = []): void
            {
                if (is_string($context['sql'] ?? null)) {
                    $this->queries[] = $context['sql'];
                }
            }

            /** @return list<string> */
            public function takeQueries(): array
            {
                $queries = $this->queries;
                $this->queries = [];

                return $queries;
            }
        };
        $config = new Configuration();
        $config->setMiddlewares([new Middleware($logger)]);
        $reader = DriverManager::getConnection($this->db->getParams(), $config);
        try {
            foreach ([$etag, '"unrelated", W/' . $etag, '*'] as $validator) {
                $cached = $this->get($kind, $validator, $reader);
                self::assertSame(304, $cached->getStatusCode());
                self::assertSame('', (string) $cached->getBody());
                self::assertSame('', $cached->getHeaderLine('Content-Type'));
                self::assertSame($etag, $cached->getHeaderLine('ETag'));
                self::assertSame('public, no-cache', $cached->getHeaderLine('Cache-Control'));
                self::assertSame('inline', $cached->getHeaderLine('Content-Disposition'));
                $queries = $logger->takeQueries();
                self::assertCount(1, $queries);
                self::assertDoesNotMatchRegularExpression('/(?:\b' . $kind . '\b|\*)/i', $queries[0]);
            }
        } finally {
            $reader->close();
        }
    }

    #[DataProvider('imageExamples')]
    public function testConcurrentReplacementUsesTheDigestOfTheBytesBeingSent(string $kind, string $type, string $bytes): void
    {
        $this->initialize();
        $this->writeImage($kind, $type, $bytes);

        $writer = Database::connect(self::fixtureDsn());
        try {
            $service = new PublicSettings(new PublicSettingsStore($this->db));
            $result = $service->image(BrandingImage::from($kind), function (string $digest) use ($kind, $type, $bytes, $writer): bool {
                self::assertSame(trim(self::etag($type, $bytes), '"'), $digest);
                $this->writeImage($kind, 'image/jpeg', self::JPEG, $writer);

                return false;
            });
        } finally {
            $writer->close();
        }

        self::assertEquals([
            'contentType' => 'image/jpeg',
            'data' => self::JPEG,
            'digest' => trim(self::etag('image/jpeg', self::JPEG), '"'),
        ], $result);
    }

    /** @return iterable<string, array{string, ?string}> */
    public static function accentExamples(): iterable
    {
        // The Go domain vectors cover the conversion and the sRGB gamut limit.
        yield 'red-purple' => ['#7a1f3d', '#ee9cae'];
        yield 'blue' => ['#1d4ed8', '#97b7f8'];
        yield 'teal' => ['#0f766e', '#72cac0'];
        yield 'orange' => ['#b45309', '#eca47e'];
        yield 'red' => ['#ff0000', '#f19f92'];
        yield 'grey' => ['#808080', '#b7b7b7'];
        yield 'black' => ['#000000', '#b7b7b7'];
        yield 'invalid' => ['#xyz', null];
    }

    #[DataProvider('accentExamples')]
    public function testDarkAccentMatchesGo(string $light, ?string $dark): void
    {
        self::assertSame($dark, AccentColor::dark($light));
    }

    public function testOfficeCanReadSchoolSettingsAndOnlyAdminCanWriteThem(): void
    {
        $this->initialize();
        $this->openSettingsSession();
        self::assertSame(405, $this->adminRequest('HEAD', 'school', signedIn: false)->getStatusCode());
        self::assertSame(401, $this->adminRequest('GET', 'school', signedIn: false)->getStatusCode());
        $this->db->executeStatement("UPDATE accounts SET role = 'member'");
        self::assertSame(403, $this->adminRequest('GET', 'school')->getStatusCode());
        $this->db->executeStatement("UPDATE accounts SET role = 'office'");
        self::assertSame(['schoolName' => 'Example Studio'], self::decoded($this->adminRequest('GET', 'school')));
        self::assertSame(403, $this->adminRequest('PUT', 'school', ['schoolName' => 'New Name'])->getStatusCode());
        $this->db->executeStatement("UPDATE accounts SET role = 'admin'");

        foreach ([[], ['schoolName' => '   '], ['schoolName' => 'Name', 'legalNotice' => str_repeat('ü', 2001)]] as $body) {
            self::assertSame(422, $this->adminRequest('PUT', 'school', $body)->getStatusCode());
        }
        $unknown = $this->adminRequest('PUT', 'school', ['schoolName' => 'Name', 'timezone' => 'UTC']);
        self::assertSame(422, $unknown->getStatusCode());
        self::assertSame([['location' => 'body.timezone', 'message' => 'unexpected property']], self::decoded($unknown)['errors']);
        self::assertSame(['schoolName' => 'Fixture Dance Hall', 'legalNotice' => "Street\r\nSecond line"], self::decoded(
            $this->adminRequest('PUT', 'school', ['schoolName' => ' Fixture Dance Hall ', 'legalNotice' => " Street\r\nSecond line\n "]),
        ));
        self::assertSame('Fixture Dance Hall', self::decoded($this->get('public'))['schoolName']);
        self::assertSame(['schoolName' => 'Fixture Dance Hall'], self::decoded(
            $this->adminRequest('PUT', 'school', ['SchoolName' => 'Fixture Dance Hall']),
        ));
        $this->db->executeStatement('DELETE FROM settings');
        self::assertSame(404, $this->adminRequest('GET', 'school')->getStatusCode());
        self::assertSame(404, $this->adminRequest('PUT', 'school', ['schoolName' => 'Example Studio'])->getStatusCode());
    }

    public function testBrandingRejectsLowContrastAndKeepsAnOmittedTheme(): void
    {
        $this->initialize();
        $this->openSettingsSession();
        $this->writeImage('logo', 'image/png', self::PNG);
        self::assertSame(['theme' => 'programme', 'hasLogo' => true, 'hasBackground' => false, 'minContrast' => 4.5], self::decoded(
            $this->adminRequest('GET', 'branding'),
        ));
        foreach ([['accentColor' => '#ff0000'], ['accentColor' => 'blue'], ['accentColor' => 42], ['theme' => ''], ['theme' => 'unknown']] as $body) {
            self::assertSame(422, $this->adminRequest('PUT', 'branding', $body)->getStatusCode());
        }
        $branding = self::decoded($this->adminRequest('PUT', 'branding', ['accentColor' => '#1D4ED8', 'theme' => 'werkstatt']));
        self::assertSame('#1d4ed8', $branding['accentColor']);
        self::assertSame('#97b7f8', $branding['accentColorDark']);
        self::assertSame('werkstatt', $branding['theme']);
        self::assertSame(['theme' => 'werkstatt', 'hasLogo' => true, 'hasBackground' => false, 'minContrast' => 4.5], self::decoded(
            $this->adminRequest('PUT', 'branding', []),
        ));
        self::assertSame('werkstatt', self::decoded($this->get('public'))['theme']);
    }

    public function testBackgroundUploadValidatesTheBytesAndUpdatesPublicImages(): void
    {
        $this->initialize();
        $this->openSettingsSession();
        $path = 'branding/background';
        $webp = ['contentType' => 'image/webp', 'data' => base64_encode(self::WEBP)];
        $this->db->executeStatement("UPDATE accounts SET role = 'office'");
        self::assertSame(403, $this->adminRequest('PUT', $path, $webp)->getStatusCode());
        self::assertSame(403, $this->adminRequest('DELETE', $path)->getStatusCode());
        $this->db->executeStatement("UPDATE accounts SET role = 'admin'");
        foreach ([
            ['contentType' => 'image/gif', 'data' => base64_encode('GIF89a')],
            ['contentType' => 'image/png', 'data' => base64_encode(self::JPEG)],
            ['contentType' => 'image/jpeg', 'data' => ''],
            ['contentType' => 'image/jpeg', 'data' => '%%%'],
            ['contentType' => 'image/jpeg', 'data' => base64_encode(self::JPEG) . "\n"],
            ['contentType' => 'image/jpeg', 'data' => rtrim(base64_encode(self::JPEG . 'x'), '=')],
            ['contentType' => 'image/png', 'data' => base64_encode(self::PNG . str_repeat('x', 1024 * 1024))],
        ] as $body) {
            self::assertSame(422, $this->adminRequest('PUT', $path, $body)->getStatusCode());
        }
        self::assertSame(404, $this->get('background')->getStatusCode());
        $boundary = str_pad(self::JPEG, 1024 * 1024, "\x00");
        foreach (['image/jpeg' => $boundary, 'image/png' => self::PNG, 'image/webp' => self::WEBP] as $type => $bytes) {
            self::assertSame(204, $this->adminRequest('PUT', $path, ['contentType' => $type, 'data' => base64_encode($bytes)])->getStatusCode());
            $asset = $this->get('background');
            self::assertSame(200, $asset->getStatusCode());
            self::assertSame($type, $asset->getHeaderLine('Content-Type'));
            self::assertSame($bytes, (string) $asset->getBody());
            self::assertSame(self::etag($type, $bytes), $asset->getHeaderLine('ETag'));
            self::assertSame(304, $this->get('background', self::etag($type, $bytes))->getStatusCode());
        }
        self::assertTrue(self::decoded($this->adminRequest('GET', 'branding'))['hasBackground']);
        self::assertTrue(self::decoded($this->get('public'))['hasBackground']);
        for ($attempt = 0; $attempt < 2; ++$attempt) {
            self::assertSame(204, $this->adminRequest('DELETE', $path)->getStatusCode());
        }
        self::assertFalse(self::decoded($this->get('public'))['hasBackground']);
        self::assertSame(404, $this->get('background')->getStatusCode());
        $this->db->executeStatement('DELETE FROM settings');
        self::assertSame(404, $this->adminRequest('DELETE', $path)->getStatusCode());
    }

    public function testRegionalSettingsValidateAndUpdateUnlockedValues(): void
    {
        $this->initialize();
        $this->openSettingsSession();
        $this->db->executeStatement('UPDATE settings SET default_locale = ?', ['de_AT']);
        $this->db->executeStatement("UPDATE accounts SET role = 'office'");
        self::assertSame([
            'timezone' => 'Europe/Vienna', 'defaultLocale' => 'de', 'currency' => 'EUR',
            'timezoneLocked' => false, 'currencyLocked' => false,
        ], self::decoded($this->adminRequest('GET', 'regional')));
        $values = ['timezone' => ' America/New_York ', 'defaultLocale' => 'de', 'currency' => 'USD'];
        self::assertSame(403, $this->adminRequest('PUT', 'regional', $values)->getStatusCode());
        $this->db->executeStatement("UPDATE accounts SET role = 'admin'");

        foreach ([['defaultLocale' => 'de-AT'], ['timezone' => 'Local'], ['timezone' => '+02:00'],
            ['timezone' => str_repeat('A', 65)], ['currency' => 'usd'], ['currency' => 'ZZZ']] as $invalid) {
            $error = $this->adminRequest('PUT', 'regional', $invalid + $values);
            self::assertSame(422, $error->getStatusCode());
            $details = self::decoded($error)['errors'];
            self::assertIsArray($details);
            self::assertIsArray($details[0]);
            self::assertSame('body.' . array_key_first($invalid), $details[0]['location']);
        }
        $expected = ['timezone' => 'America/New_York', 'defaultLocale' => 'de', 'currency' => 'USD'];
        self::assertSame($expected + ['timezoneLocked' => false, 'currencyLocked' => false], self::decoded(
            $this->adminRequest('PUT', 'regional', $values),
        ));
        self::assertSame($expected, array_intersect_key(self::decoded($this->get('public')), $expected));

        $this->db->executeStatement('DELETE FROM settings');
        self::assertSame(404, $this->adminRequest('GET', 'regional')->getStatusCode());
        self::assertSame(404, $this->adminRequest('PUT', 'regional', $values)->getStatusCode());
    }

    public function testRegionalSettingsLockChangesWhenLessonsOrPricesExist(): void
    {
        $this->initialize();
        $this->openSettingsSession();
        $stamp = '2026-11-19 12:00:00';
        $values = ['timezone' => 'Europe/Vienna', 'defaultLocale' => 'en', 'currency' => 'EUR'];
        $passType = $this->insertRegionalFixture('pass_types', [
            'name' => 'Test pass', 'kind' => 'visits', 'visits' => 2, 'validity_days' => 30,
            'price' => 100, 'vat_rate_bp' => 0, 'active' => true, 'created_at' => $stamp,
        ]);
        $currencyLocked = ['timezoneLocked' => false, 'currencyLocked' => true];
        $this->assertRegionalSettings($values + $currencyLocked);
        $this->db->executeStatement('UPDATE pass_types SET price = 0');
        $this->assertRegionalSettings($values + ['timezoneLocked' => false, 'currencyLocked' => false]);

        $this->insertRegionalFixture('passes', [
            'pass_type_id' => $passType, 'person_id' => $this->db->fetchOne('SELECT id FROM persons'),
            'kind' => 'visits', 'visits_total' => 2, 'visits_remaining' => 2, 'valid_from' => '2026-11-19',
            'status' => 'active', 'origin' => 'sale', 'sold_at' => $stamp, 'price_paid' => 100,
        ]);
        $this->assertRegionalSettings($values + $currencyLocked);
        $this->db->executeStatement('DELETE FROM passes');
        $course = $this->insertRegionalFixture('course_types', ['name' => 'Fixture ballet']);
        $event = $this->insertRegionalFixture('events', [
            'slug' => 'test-workshop', 'title' => 'Test workshop', 'course_type_id' => $course,
            'capacity' => 10, 'price' => 100, 'vat_rate_bp' => 0, 'status' => 'draft',
            'created_at' => $stamp, 'updated_at' => $stamp,
        ]);
        $this->assertRegionalSettings($values + $currencyLocked);
        $this->db->executeStatement('UPDATE events SET price = 0');
        $this->insertRegionalFixture('event_registrations', [
            'event_id' => $event, 'guest_name' => 'Fixture Guest', 'status' => 'registered', 'payment' => 'unpaid',
            'price' => 100, 'vat_rate_bp' => 0, 'created_at' => $stamp, 'updated_at' => $stamp,
        ]);
        $this->assertRegionalSettings($values + $currencyLocked);

        $location = $this->insertRegionalFixture('locations', ['name' => 'Fixture location']);
        $room = $this->insertRegionalFixture('rooms', ['name' => 'Room', 'capacity' => 10, 'location_id' => $location, 'position' => 1]);
        $staff = $this->insertRegionalFixture('staff', ['display_name' => 'Fixture instructor', 'kind' => 'instructor', 'active' => true]);
        $this->insertRegionalFixture('lessons', [
            'course_type_id' => $course, 'room_id' => $room, 'instructor_id' => $staff,
            'starts_at' => $stamp, 'ends_at' => '2026-11-19 13:00:00', 'status' => 'scheduled',
            'visibility' => 'public', 'kind' => 'regular', 'capacity' => 10,
        ]);
        $locked = ['timezoneLocked' => true, 'currencyLocked' => true];
        $this->assertRegionalSettings($values + $locked);

        foreach (['timezone' => 'Europe/Berlin', 'currency' => 'USD'] as $field => $replacement) {
            $response = $this->adminRequest('PUT', 'regional', [$field => $replacement, 'defaultLocale' => 'de'] + $values);
            self::assertSame(409, $response->getStatusCode());
            self::assertSame($field . '-locked', self::decoded($response)['reason']);
        }
        $this->assertRegionalSettings($values + $locked);
        $update = ['defaultLocale' => 'de'] + $values;
        self::assertEquals($update + $locked, self::decoded($this->adminRequest('PUT', 'regional', $update)));
    }

    public function testBookingRulesReadStoredCutoffsAndWindow(): void
    {
        $this->initialize();
        $clock = self::createStub(ClockInterface::class);
        $now = new DateTimeImmutable('2026-10-23T18:00:00.123456+02:00');
        $clock->method('now')->willReturn($now);
        $this->openSettingsSession($now);
        $this->db->executeStatement("UPDATE accounts SET role = 'office'");
        $postgres = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $book = $postgres ? "INTERVAL '119.9 seconds'" : '119900000';
        $invite = $postgres ? "INTERVAL '3 hours 2 minutes 1 second'" : '10921000000';
        $this->db->executeStatement(
            "UPDATE settings SET booking_cutoff = $book, waitlist_invite_expiry = $invite, released_through = '2026-11-20'",
        );
        $path = 'booking-rules';
        self::assertSame(401, $this->adminRequest('GET', $path, signedIn: false, clock: $clock)->getStatusCode());
        $rules = self::decoded($this->adminRequest('GET', $path, clock: $clock));
        self::assertEquals([
            'releaseWeekday' => 0, 'releaseTime' => '18:00', 'releaseWeeksAhead' => 2,
            'bookingCutoffMinutes' => 1, 'cancellationCutoffMinutes' => 120, 'waitlistInviteExpiryMinutes' => 182,
            'defaultVisibleDays' => 14, 'releasedThrough' => '2026-11-20', 'generatedLessons' => 0,
            'window' => [
                'now' => '2026-10-23T16:00:00.123456Z', 'lastReleaseAt' => '2026-10-18T16:00:00Z',
                'nextReleaseAt' => '2026-10-25T17:00:00Z', 'releasedUntil' => '2026-11-20T23:00:00Z',
                'ruleReleasedUntil' => '2026-11-01T23:00:00Z',
            ],
        ], $rules);
        $this->db->executeStatement('DELETE FROM settings');
        self::assertSame(404, $this->adminRequest('GET', $path, clock: $clock)->getStatusCode());
    }

    public function testBookingRulesPreviewUsesSchoolDatesWithoutSavingChanges(): void
    {
        $this->initialize();
        $clock = self::createStub(ClockInterface::class);
        $now = new DateTimeImmutable('2026-10-25T01:00:00Z');
        $clock->method('now')->willReturn($now);
        $this->openSettingsSession($now);
        $this->db->executeStatement("UPDATE accounts SET role = 'member'");
        $path = 'booking-rules/preview';
        $query = 'releaseWeekday=0&releaseTime=02:30&releaseWeeksAhead=1';
        self::assertSame(403, $this->adminRequest('GET', "$path?$query", clock: $clock)->getStatusCode());
        $this->db->executeStatement("UPDATE accounts SET role = 'office'");
        self::assertSame(405, $this->adminRequest('HEAD', "$path?$query", clock: $clock)->getStatusCode());
        $missing = $this->adminRequest('GET', $path, clock: $clock);
        self::assertSame(422, $missing->getStatusCode());
        $missingDetails = self::decoded($missing)['errors'];
        self::assertIsArray($missingDetails);
        self::assertSame(['query.releaseWeekday', 'query.releaseTime', 'query.releaseWeeksAhead'], array_column($missingDetails, 'location'));
        $before = self::decoded($this->adminRequest('GET', 'booking-rules', clock: $clock));
        self::assertArrayNotHasKey('releasedThrough', $before);
        $window = [
            'now' => '2026-10-25T01:00:00Z', 'lastReleaseAt' => '2026-10-25T00:30:00Z',
            'nextReleaseAt' => '2026-11-01T01:30:00Z', 'releasedUntil' => '2026-11-01T23:00:00Z',
            'ruleReleasedUntil' => '2026-11-01T23:00:00Z',
        ];
        self::assertEquals($window, self::decoded($this->adminRequest('GET', "$path?$query", clock: $clock)));
        self::assertEquals($window, self::decoded($this->adminRequest('GET', "$path?$query&releasedThrough=2026-10-20", clock: $clock)));
        self::assertEquals(['releasedUntil' => '2026-11-03T23:00:00Z'] + $window, self::decoded(
            $this->adminRequest('GET', "$path?$query&releasedThrough=2026-11-03", clock: $clock),
        ));
        $lateClock = self::createStub(ClockInterface::class);
        $lateClock->method('now')->willReturn(new DateTimeImmutable('2026-10-25T23:30:00Z'));
        // The school date is October 26, so October 25 next year is exactly 52 weeks ahead.
        self::assertSame(200, $this->adminRequest('GET', "$path?$query&releasedThrough=2027-10-25", clock: $lateClock)->getStatusCode());
        self::assertSame(422, $this->adminRequest('GET', "$path?$query&releasedThrough=2027-10-26", clock: $lateClock)->getStatusCode());
        $invalid = [
            'releaseWeekday' => 'releaseTime=02:30&releaseWeeksAhead=1',
            'releaseTime' => str_replace('02:30', '25:00', $query),
            'releaseWeeksAhead' => str_replace('Ahead=1', 'Ahead=53', $query),
            'releasedThrough' => "$query&releasedThrough=2027-12-01",
        ];
        foreach ($invalid as $field => $params) {
            $response = $this->adminRequest('GET', "$path?$params", clock: $clock);
            self::assertSame(422, $response->getStatusCode());
            $details = self::decoded($response)['errors'];
            self::assertIsArray($details);
            self::assertIsArray($details[0]);
            self::assertSame('query.' . $field, $details[0]['location']);
        }
        self::assertSame(422, $this->adminRequest('GET', "$path?$query&releasedThrough=2026-02-30", clock: $clock)->getStatusCode());
        self::assertSame($before, self::decoded($this->adminRequest('GET', 'booking-rules', clock: $clock)));
    }

    /** @param array<string, string|bool> $expected */
    private function assertRegionalSettings(array $expected): void
    {
        self::assertSame($expected, self::decoded($this->adminRequest('GET', 'regional')));
    }

    /** @param array<string, mixed> $values */
    private function insertRegionalFixture(string $table, array $values): string
    {
        $uuid = Uuid::v7();
        $id = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform ? $uuid->toRfc4122() : $uuid->toBinary();
        $types = isset($values['active']) ? ['active' => ParameterType::BOOLEAN] : [];
        $this->db->insert($table, ['id' => $id] + $values, $types);

        return $id;
    }

    private function openSettingsSession(?DateTimeImmutable $now = null): void
    {
        $postgres = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $id = Uuid::v7();
        $digest = $postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $now ??= new DateTimeImmutable();
        $created = Database::sqlInstant($this->db, $now);
        $this->db->executeStatement(
            'INSERT INTO sessions (id, account_id, token_hash, created_at, last_seen_at, expires_at) '
            . "SELECT ?, id, $digest, ?, ?, ? FROM accounts",
            [$postgres ? $id->toRfc4122() : $id->toBinary(), hash('sha256', 'fixture-settings-session'),
                $created, $created, Database::sqlInstant($this->db, $now->modify('+1 day'))],
        );
        $this->db->executeStatement('UPDATE accounts SET confirmed_at = ?', [$created]);
    }

    /** @param array<string, mixed>|null $body */
    private function adminRequest(string $method, string $path, ?array $body = null, bool $signedIn = true, ?ClockInterface $clock = null): ResponseInterface
    {
        $request = (new ServerRequestFactory())->createServerRequest($method, '/studio/api/v1/admin/settings/' . $path);
        if ($signedIn) {
            $request = $request->withCookieParams(['studio_session' => 'fixture-settings-session']);
        }
        if ($body !== null) {
            $request = $request->withHeader('Content-Type', 'application/json');
            $request->getBody()->write(json_encode((object) $body, JSON_THROW_ON_ERROR));
            $request->getBody()->rewind();
        }

        return $this->bootstrapRequest($request, $clock);
    }

    private static function fixtureDsn(): string
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database provisioned by just php-test');
        }

        return $dsn;
    }

    private function initialize(): void
    {
        $setup = new SchoolSetup(new SetupStore($this->db), new SystemClock());
        $setup->init(new SetupInput('Example Studio', 'Europe/Vienna', 'en', 'EUR', 'admin@example.org', 'Ada', 'Example'));
    }

    private function get(string $path, string $ifNoneMatch = '', ?Connection $connection = null): ResponseInterface
    {
        $request = (new ServerRequestFactory())->createServerRequest('GET', '/studio/api/v1/settings/' . $path)
            ->withHeader('If-None-Match', $ifNoneMatch);
        if ($connection === null) {
            return $this->bootstrapRequest($request);
        }

        $service = new PublicSettings(new PublicSettingsStore($connection));
        $app = AppFactory::create(new ResponseFactory());
        $app->setBasePath('/studio');
        (new PublicSettingsEndpoint($service))->register($app);
        $app->addRoutingMiddleware();

        return $app->handle($request);
    }

    private function bootstrapRequest(ServerRequestInterface $request, ?ClockInterface $clock = null): ResponseInterface
    {
        return Bootstrap::handle(
            static fn() => $request,
            dirname(__DIR__),
            __DIR__ . '/../public',
            self::postgresMigrations(),
            static fn(string $key): string => match ($key) {
                'BASE_URL' => 'http://localhost/studio',
                'DATABASE_URL' => self::fixtureDsn(),
                'LOG_LEVEL' => 'error',
                default => '',
            },
            clock: $clock,
        );
    }

    private static function postgresMigrations(): string
    {
        return getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: __DIR__ . '/../../internal/store/postgres/migrations';
    }

    private function writeImage(string $kind, string $type, string $bytes, ?Connection $connection = null): void
    {
        ($connection ?? $this->db)->executeStatement(
            "UPDATE settings SET {$kind} = ?, {$kind}_type = ?, {$kind}_digest = ?",
            [$bytes, $type, trim(self::etag($type, $bytes), '"')],
            [ParameterType::LARGE_OBJECT, ParameterType::STRING, ParameterType::STRING],
        );
    }

    private static function etag(string $type, string $bytes): string
    {
        return '"' . hash('sha256', $type . "\n" . $bytes) . '"';
    }

    /** @return array<array-key, mixed> */
    private static function decoded(ResponseInterface $response): array
    {
        $body = json_decode((string) $response->getBody(), true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($body);

        return $body;
    }
}
