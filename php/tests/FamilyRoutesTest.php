<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\DriverException;
use Doctrine\DBAL\Exception\LockWaitTimeoutException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\Psr7\Factory\StreamFactory;
use StudioSchedule\Bootstrap;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\FamilyStore;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;

final class FamilyRoutesTest extends TestCase
{
    private const string BASE = 'https://school.example.test/dance';
    private const string NOW = '2026-11-18T23:30:00Z';

    private Connection $db;
    private bool $postgres;
    private string $dsn;
    private string $postgresMigrations;
    private bool $fixture = false;

    protected function setUp(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires a disposable database from just php-test');
        }
        $this->dsn = $dsn;
        $this->db = Database::connect($dsn);
        $this->postgres = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $this->postgresMigrations = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations';
        self::assertSame([], (new Schema($this->db))->relations(), 'Requires an empty disposable database');
        $this->fixture = true;
        (new Migrator($dsn))->up($this->postgres ? $this->postgresMigrations : dirname(__DIR__) . '/migrations/mysql');
        $duration = $this->postgres ? "'0 seconds'" : '0';
        $this->db->executeStatement(
            'INSERT INTO settings (school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time, '
            . 'release_weeks_ahead, default_visible_days, booking_cutoff, cancellation_cutoff) '
            . "VALUES ('Example Studio', 'Europe/Berlin', 'en', 'EUR', 'R', 0, '18:00:00', 1, 2, $duration, $duration)",
        );
        foreach ([
            1 => ['Mara', 'Example'], 2 => ['Teen', 'Zebra'], 3 => ['Kid', 'Alpha'], 4 => ['Teacher', 'Beta'],
            5 => ['New Parent', 'Beta'], 6 => ['Other', 'Guardian'], 7 => ['Outsider', 'Example'],
            8 => ['Grandchild', 'Example'], 9 => ['Staff', 'Admin'], 10 => ['Deleted', 'person'],
        ] as $id => [$given, $family]) {
            $this->db->executeStatement('INSERT INTO persons (id, given_name, family_name) VALUES (?, ?, ?)', [$this->uuid($id), $given, $family]);
        }
        $this->db->executeStatement(
            "UPDATE persons SET email = 'private-contact@example.test', notes = 'office-only note', phone = '12345', birth_date = '2000-12-01' WHERE id = ?",
            [$this->uuid(1)],
        );
        $this->db->executeStatement("UPDATE persons SET anonymized_at = '2026-11-17 10:00:00' WHERE id = ?", [$this->uuid(10)]);
        foreach ([2, 3, 4, 5, 10] as $dependent) {
            $this->link(1, $dependent);
        }
        $this->link(5, 8);
        $this->link(6, 3);
        $this->db->executeStatement("INSERT INTO staff (id, person_id, display_name, kind, active) VALUES (?, ?, 'Fixture teacher', 'instructor', FALSE)", [$this->uuid(21), $this->uuid(4)]);
        $this->account(1, 'member');
        $this->account(2, 'member', disabled: true);
        $this->account(7, 'member');
        $this->account(9, 'admin');
        $this->session(1, 'parent', 31);
        $this->session(7, 'outsider', 37);
        $this->session(1, 'acting-parent', 39, 9);
    }

    protected function tearDown(): void
    {
        if (!$this->fixture) {
            return;
        }
        try {
            if (!$this->postgres) {
                $this->db->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            foreach ($this->db->createSchemaManager()->listTableNames() as $table) {
                $this->db->executeStatement('DROP TABLE ' . $this->db->quoteSingleIdentifier($table) . ($this->postgres ? ' CASCADE' : ''));
            }
            if ($this->postgres) {
                $this->db->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
        } finally {
            $this->db->close();
        }
    }

    public function testFamilyAndWritesUseTheEffectivePersonAndProtectManagedDependents(): void
    {
        $this->request('GET', '', user: null, status: 401);
        $this->request('POST', '/dependents', '{', user: null, status: 401);
        $family = $this->request('GET', '');
        self::assertSame([
            'id' => self::id(1), 'givenName' => 'Mara', 'familyName' => 'Example', 'phone' => '12345', 'birthDate' => '2000-12-01',
        ], $family['me']);
        self::assertIsArray($family['dependents']);
        self::assertSame(array_map(self::id(...), [3, 5, 4, 2, 10]), array_column($family['dependents'], 'id'));
        self::assertSame([false, true, true, true, false], array_column($family['dependents'], 'managed'));
        self::assertSame([[['givenName' => 'Other', 'familyName' => 'Guardian']], [], [], [], []], array_column($family['dependents'], 'guardians'));
        self::assertSame($family, $this->request('GET', '', user: 'acting-parent'));
        self::assertSame([], $this->request('GET', '', user: 'outsider')['dependents']);

        $profile = $this->request('PUT', '/me', ['givenName' => " Mara\t", 'familyName' => 'Updated', 'birthDate' => '2026-11-19'], user: 'acting-parent');
        self::assertSame(['id' => self::id(1), 'givenName' => 'Mara', 'familyName' => 'Updated', 'birthDate' => '2026-11-19'], $profile);
        self::assertSame('Staff', $this->db->fetchOne('SELECT given_name FROM persons WHERE id = ?', [$this->uuid(9)]));
        self::assertSame([
            'email' => 'private-contact@example.test', 'notes' => 'office-only note', 'phone' => null,
        ], $this->db->fetchAssociative('SELECT email, notes, phone FROM persons WHERE id = ?', [$this->uuid(1)]));

        $childInput = ['givenName' => 'New Child', 'familyName' => 'Example', 'phone' => ' 0123 '];
        $newChild = $this->request('POST', '/dependents', $childInput, status: 201);
        self::assertIsString($newChild['id']);
        self::assertMatchesRegularExpression('/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/D', $newChild['id']);
        self::assertSame('0123', $newChild['phone']);
        $family = $this->request('GET', '');
        self::assertIsArray($family['dependents']);
        self::assertContains($newChild['id'], array_column($family['dependents'], 'id'));

        $edit = ['givenName' => 'New Name', 'familyName' => 'Alpha', 'phone' => null, 'birthDate' => null];
        self::assertSame(['id' => self::id(3), 'givenName' => 'New Name', 'familyName' => 'Alpha'], $this->request('PUT', '/dependents/' . self::id(3), $edit));
        foreach ([2, 4, 5] as $id) {
            self::assertSame('managed', $this->request('PUT', '/dependents/' . self::id($id), $edit, status: 409)['reason']);
        }
        foreach ([7, 10, 99] as $id) {
            $this->request('PUT', '/dependents/' . self::id($id), $edit, status: 404);
        }
        $this->request('PUT', '/dependents/' . self::id(3), $edit, user: 'outsider', status: 404);

        // A failed link insert must roll back the newly created person.
        $this->db->executeStatement('ALTER TABLE person_guardians RENAME TO fixture_guardians');
        try {
            $this->request('POST', '/dependents', ['givenName' => 'Rolled Back', 'familyName' => 'Example'], status: 500);
            self::assertContains($this->db->fetchOne("SELECT COUNT(*) FROM persons WHERE given_name = 'Rolled Back'"), [0, '0']);
        } finally {
            $this->db->executeStatement('ALTER TABLE fixture_guardians RENAME TO person_guardians');
        }
    }

    public function testInvalidFamilyFieldsUseTheGoProblemLocations(): void
    {
        $person = ['givenName' => 'Mara', 'familyName' => 'Example'];
        foreach ([
            ['givenName', " \t", 'body.givenName'], ['familyName', null, 'body.familyName'],
            ['phone', str_repeat('é', 51), 'body.phone'], ['birthDate', '2026-02-30', 'body.birthDate'],
            ['birthDate', '2026-11-20', 'body.birthDate'], ['birthDate', '', 'body.birthDate'], ['notes', 'private', 'body.notes'],
        ] as [$field, $value, $location]) {
            $problem = $this->request('PUT', '/me', [$field => $value] + $person, status: 422);
            self::assertIsArray($problem['errors']);
            self::assertContains($location, array_column($problem['errors'], 'location'));
        }
        $this->request('PUT', '/dependents/zero', $person, status: 422);
        $missing = $this->request('PUT', '/me', ['familyName' => 'Example'], status: 422);
        self::assertIsArray($missing['errors']);
        self::assertContains('body', array_column($missing['errors'], 'location'));
        $this->request('PUT', '/me', '{', status: 400);
        $this->db->executeStatement('DELETE FROM settings');
        $this->request('PUT', '/me', $person + ['birthDate' => '2000-01-01'], status: 404);
        $this->request('PUT', '/me', $person);
    }

    public function testGuardianshipRecheckWaitsForAConcurrentRevocation(): void
    {
        $reader = Database::connect($this->dsn);
        $reader->executeStatement($this->postgres ? "SET lock_timeout = '250ms'" : 'SET innodb_lock_wait_timeout = 1');
        $family = new FamilyStore($reader);
        $this->db->beginTransaction();
        try {
            $this->db->executeStatement('DELETE FROM person_guardians WHERE guardian_id = ? AND dependent_id = ?', [$this->uuid(1), $this->uuid(3)]);
            // A nonlocking membership check can still see the old version of the link.
            self::assertTrue($family->isGuardian(self::id(1), self::id(3)));
            $reader->beginTransaction();
            try {
                self::assertTrue($family->lockLivePersons(self::id(1), self::id(3)));
                try {
                    $family->isGuardian(self::id(1), self::id(3), forUpdate: true);
                    self::fail('The membership recheck should wait for the competing deletion to finish');
                } catch (DriverException $error) {
                    if ($this->postgres) {
                        self::assertSame('55P03', $error->getSQLState());
                    } else {
                        self::assertInstanceOf(LockWaitTimeoutException::class, $error);
                    }
                }
            } finally {
                $reader->rollBack();
            }
            $this->db->commit();
            $this->request('PUT', '/dependents/' . self::id(3), ['givenName' => 'Outdated', 'familyName' => 'Edit'], status: 404);
        } finally {
            if ($this->db->isTransactionActive()) {
                $this->db->rollBack();
            }
            $reader->close();
        }
    }

    private function account(int $id, string $role, bool $disabled = false): void
    {
        $stamp = '2026-11-18 12:00:00';
        $this->db->executeStatement('INSERT INTO accounts (id, person_id, email, role, confirmed_at, disabled_at) VALUES (?, ?, ?, ?, ?, ?)', [
            $this->uuid($id), $this->uuid($id), "person-$id@example.test", $role, $stamp, $disabled ? $stamp : null,
        ]);
    }

    private function session(int $account, string $name, int $session, ?int $impersonator = null): void
    {
        $hash = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->db->executeStatement(
            "INSERT INTO sessions (id, account_id, token_hash, created_at, last_seen_at, expires_at, impersonator_id) VALUES (?, ?, $hash, ?, ?, ?, ?)",
            [$this->uuid($session), $this->uuid($account), hash('sha256', 'fixture-' . $name), '2026-11-18 23:00:00',
                '2026-11-18 23:00:00', '2026-11-19 01:00:00', $impersonator === null ? null : $this->uuid($impersonator)],
        );
    }

    private function link(int $guardian, int $dependent): void
    {
        $this->db->executeStatement('INSERT INTO person_guardians (guardian_id, dependent_id) VALUES (?, ?)', [$this->uuid($guardian), $this->uuid($dependent)]);
    }

    /**
     * @param array<string,mixed>|string|null $input
     * @return array<array-key,mixed>
     */
    private function request(string $method, string $path, array|string|null $input = null, ?string $user = 'parent', int $status = 200): array
    {
        $request = (new ServerRequestFactory())->createServerRequest($method, self::BASE . '/api/v1/family' . $path);
        if ($user !== null) {
            $request = $request->withCookieParams(['studio_session' => 'fixture-' . $user]);
        }
        if ($input !== null) {
            $request = $request->withHeader('Content-Type', 'application/json')
                ->withBody((new StreamFactory())->createStream(is_array($input) ? json_encode($input, JSON_THROW_ON_ERROR) : $input));
        }
        $env = ['DATABASE_URL' => $this->dsn, 'BASE_URL' => self::BASE, 'LOG_LEVEL' => 'error'];
        $clock = new class (self::NOW) implements ClockInterface {
            public function __construct(private readonly string $now) {}

            public function now(): DateTimeImmutable
            {
                return new DateTimeImmutable($this->now);
            }
        };
        $response = Bootstrap::handle(
            static fn() => $request,
            dirname(__DIR__),
            '/missing',
            $this->postgresMigrations,
            static fn(string $key): ?string => $env[$key] ?? null,
            clock: $clock,
        );
        self::assertSame($status, $response->getStatusCode(), (string) $response->getBody());
        self::assertSame('no-store', $response->getHeaderLine('Cache-Control'));
        $body = json_decode((string) $response->getBody(), true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($body);

        return $body;
    }

    private function uuid(int $id): string
    {
        $text = self::id($id);

        return $this->postgres ? $text : pack('H*', str_replace('-', '', $text));
    }

    private static function id(int $id): string
    {
        return sprintf('018f0000-0000-7000-8000-%012x', $id);
    }
}
