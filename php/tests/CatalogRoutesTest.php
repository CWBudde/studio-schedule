<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Container\ContainerInterface;
use Slim\App;
use Slim\Factory\AppFactory;
use Slim\Psr7\Factory\ServerRequestFactory;
use StudioSchedule\Http\CatalogRoutes;
use StudioSchedule\Store\CatalogStore;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;

final class CatalogRoutesTest extends TestCase
{
    public function testMalformedRoomLocationsDoNotReadTheDatabase(): void
    {
        self::assertTrue(class_exists(CatalogRoutes::class), 'The public catalogue needs routes through Slim');
        $app = self::app(Database::connect('postgresql://fixture:fixture@127.0.0.1:1/unused'));

        foreach (['misdirected', '00000000-0000-0000-0000-000000000000'] as $value) {
            $body = self::get($app, '/rooms?location=' . $value, 422);
            self::assertSame([
                ['location' => 'query.location', 'message' => 'invalid value: validation failed: invalid ID "' . $value . '"'],
            ], $body['errors']);
            self::assertSame('validation failed', $body['detail']);
        }
    }

    public function testCatalogueOrderingVisibilityAndLocationFilter(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database provisioned by just php-test');
        }
        $database = Database::connect($dsn);
        $postgres = $database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        self::assertSame([], (new Schema($database))->relations(), 'Catalogue tests require an empty disposable database');

        try {
            $migrations = $postgres
                ? (getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: __DIR__ . '/../../internal/store/postgres/migrations')
                : __DIR__ . '/../migrations/mysql';
            (new Migrator($dsn))->up($migrations);
            $app = self::app($database);

            foreach (['locations', 'rooms', 'staff', 'course-types', 'levels', 'terms'] as $collection) {
                self::assertSame([], self::get($app, '/' . $collection)['items']);
            }

            $id = $postgres ? '?' : "UNHEX(REPLACE(?, '-', ''))";
            $first = self::id(1);
            $second = self::id(2);
            $empty = self::id(3);
            $room1 = self::id(4);
            $room2 = self::id(5);
            $room3 = self::id(6);
            $database->executeStatement("INSERT INTO locations (id, name, address) VALUES ($id, 'Ä Studio', 'Fictional Road 1'), ($id, 'Z Studio', NULL), ($id, 'a Empty Studio', NULL)", [$first, $second, $empty]);
            $database->executeStatement("INSERT INTO rooms (id, location_id, name, capacity, position) VALUES ($id, $id, 'Room A', 10, 2), ($id, $id, 'Room Z', 20, 1), ($id, $id, 'Room B', 18, 1)", [$room1, $first, $room2, $first, $room3, $second]);
            $database->executeStatement("INSERT INTO persons (id, given_name, family_name) VALUES ($id, 'Ada', 'Example')", [self::id(7)]);
            $database->executeStatement("INSERT INTO staff (id, person_id, display_name, kind, active) VALUES ($id, $id, 'Z Instructor', 'instructor', TRUE), ($id, NULL, 'A Retired', 'instructor', FALSE), ($id, NULL, 'ä Accompanist', 'accompanist', TRUE)", [self::id(8), self::id(7), self::id(9), self::id(10)]);
            $database->executeStatement("INSERT INTO course_types (id, name, description) VALUES ($id, '2 Ballet', 'Fictional description'), ($id, '10 Dance', NULL)", [self::id(11), self::id(12)]);
            $database->executeStatement("INSERT INTO levels (id, name, code, position) VALUES ($id, 'Advanced', 'A', 2), ($id, 'Beginner', NULL, 1)", [self::id(13), self::id(14)]);
            $database->executeStatement("INSERT INTO terms (id, name, starts_on, ends_on) VALUES ($id, 'Second', '2026-10-01', '2026-12-31'), ($id, 'First', '2026-05-21', '2026-09-30')", [self::id(15), self::id(16)]);

            if ($postgres) {
                $database->executeStatement("SET DateStyle TO 'SQL, DMY'");
            }

            self::assertSame([
                ['id' => $second, 'name' => 'Z Studio'],
                ['id' => $empty, 'name' => 'a Empty Studio'],
                ['id' => $first, 'name' => 'Ä Studio', 'address' => 'Fictional Road 1'],
            ], self::get($app, '/locations')['items']);
            $locationRooms = [
                ['id' => $room2, 'locationId' => $first, 'name' => 'Room Z', 'capacity' => 20, 'position' => 1],
                ['id' => $room1, 'locationId' => $first, 'name' => 'Room A', 'capacity' => 10, 'position' => 2],
            ];
            $rooms = [
                ['id' => $room3, 'locationId' => $second, 'name' => 'Room B', 'capacity' => 18, 'position' => 1],
                ...$locationRooms,
            ];
            self::assertSame($rooms, self::get($app, '/rooms')['items']);
            self::assertSame($rooms, self::get($app, '/rooms?location=')['items']);
            self::assertSame($rooms, self::get($app, '/rooms?location=%ZZ')['items']);
            self::assertSame($rooms, self::get($app, '/rooms?location&x=y')['items']);
            self::assertSame($locationRooms, self::get($app, '/rooms?location=' . strtoupper($first) . '&location=' . $second)['items']);
            self::assertSame([], self::get($app, '/rooms?location=' . $empty)['items']);
            self::assertSame('resource not found', self::get($app, '/rooms?location=' . self::id(99), 404)['detail']);

            self::assertSame([
                ['id' => self::id(8), 'displayName' => 'Z Instructor', 'kind' => 'instructor'],
                ['id' => self::id(10), 'displayName' => 'ä Accompanist', 'kind' => 'accompanist'],
            ], self::get($app, '/staff')['items']);
            self::assertSame([
                ['id' => self::id(12), 'name' => '10 Dance'],
                ['id' => self::id(11), 'name' => '2 Ballet', 'description' => 'Fictional description'],
            ], self::get($app, '/course-types')['items']);
            self::assertSame([
                ['id' => self::id(14), 'name' => 'Beginner', 'position' => 1],
                ['id' => self::id(13), 'name' => 'Advanced', 'code' => 'A', 'position' => 2],
            ], self::get($app, '/levels')['items']);
            self::assertSame([
                ['id' => self::id(16), 'name' => 'First', 'startsOn' => '2026-05-21', 'endsOn' => '2026-09-30'],
                ['id' => self::id(15), 'name' => 'Second', 'startsOn' => '2026-10-01', 'endsOn' => '2026-12-31'],
            ], self::get($app, '/terms')['items']);
        } finally {
            if (!$postgres) {
                $database->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            foreach ($database->createSchemaManager()->listTableNames() as $table) {
                $suffix = $postgres ? ' CASCADE' : '';
                $database->executeStatement('DROP TABLE IF EXISTS ' . $database->quoteSingleIdentifier($table) . $suffix);
            }
            if ($postgres) {
                $database->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
            $database->close();
        }
    }

    private static function id(int $number): string
    {
        return sprintf('01890a5d-ac96-774b-bcce-%012x', $number);
    }

    /** @return App<ContainerInterface|null> */
    private static function app(Connection $database): App
    {
        $app = AppFactory::create();
        $app->setBasePath('/dance/schedule');
        CatalogRoutes::register($app, new CatalogStore($database));
        $app->addRoutingMiddleware();

        return $app;
    }

    /**
     * @param App<ContainerInterface|null> $app
     * @return array<string, mixed>
     */
    private static function get(App $app, string $path, int $status = 200): array
    {
        $uri = '/dance/schedule/api/v1' . $path;
        $query = parse_url($uri, PHP_URL_QUERY);
        $request = (new ServerRequestFactory())->createServerRequest('GET', $uri, ['QUERY_STRING' => $query])
            ->withAttribute('requestId', 'catalogue-fixture');
        $response = $app->handle($request);
        self::assertSame($status, $response->getStatusCode(), (string) $response->getBody());
        self::assertSame($status === 200 ? 'application/json' : 'application/problem+json', $response->getHeaderLine('Content-Type'));
        $body = json_decode((string) $response->getBody(), true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($body);
        if ($status !== 200) {
            self::assertSame('catalogue-fixture', $body['requestId']);
        }
        /** @var array<string, mixed> $body */
        return $body;
    }
}
