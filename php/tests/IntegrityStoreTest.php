<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception as DatabaseError;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Console\Operator;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\IntegrityStore;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;

final class IntegrityStoreTest extends TestCase
{
    private const CREATED = '2026-11-01 12:00:00';
    private const ANONYMIZED = '2026-11-02 12:00:00';
    private const LATER = '2026-11-03 12:00:00';
    private const CHECKS = ['passes', 'capacity', 'tombstones', 'receipt-counters'];

    private Connection $db;
    private bool $postgres;
    private string $dsn;
    private string $pgMigrations;

    protected function setUp(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database from just php-test');
        }
        $this->dsn = $dsn;
        $this->db = Database::connect($dsn);
        $this->postgres = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        self::assertSame([], (new Schema($this->db))->relations(), 'Requires an empty disposable database');
        $this->pgMigrations = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations';
        (new Migrator($dsn))->up($this->postgres ? $this->pgMigrations : dirname(__DIR__) . '/migrations/mysql');
    }

    protected function tearDown(): void
    {
        if (!isset($this->db)) {
            return;
        }
        if (!$this->postgres) {
            $this->db->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
        }
        foreach ($this->db->createSchemaManager()->listTableNames() as $table) {
            $this->db->executeStatement('DROP TABLE IF EXISTS ' . $this->db->quoteSingleIdentifier($table) . ($this->postgres ? ' CASCADE' : ''));
        }
        if ($this->postgres) {
            $this->db->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
        }
        $this->db->close();
    }

    public function testMatchesGoChecksForAllowedHistoricalReferencesAndActualInconsistencies(): void
    {
        $store = new IntegrityStore($this->db);
        self::assertSame([], Database::inReadSnapshot($this->db, static fn() => $store->findings(self::CHECKS)));

        self::assertSame([0, "no findings: passes, receipt-counters\n", ''], $this->report(['receipt-counters,passes', 'passes']));
        $this->seedBrokenSchool();
        $findings = Database::inReadSnapshot($this->db, static fn() => $store->findings(self::CHECKS));
        self::assertSame(self::expectedFindings(), $findings);
        self::assertSame([$findings[2]], Database::inReadSnapshot($this->db, static fn() => $store->findings(['capacity'])));
        self::assertSame($findings, Database::inReadSnapshot($this->db, static fn() => $store->findings(self::CHECKS)), 'Checks preserve the data');

        [$code, $stdout, $stderr] = $this->report([], 'json');
        self::assertSame(1, $code);
        self::assertSame('', $stderr);
        self::assertSame(['ok' => false, 'checks' => self::CHECKS, 'findings' => $findings], json_decode($stdout, true, flags: JSON_THROW_ON_ERROR));
        self::assertStringNotContainsString('Fictional', $stdout);
        self::assertStringNotContainsString('@example.test', $stdout);

        [$code, $stdout, $stderr] = $this->report(['capacity']);
        self::assertSame(1, $code);
        self::assertSame("capacity  lesson  " . self::id(30) . "  " . $findings[2]['message'] . "\n", $stdout);
        self::assertSame("error: 1 finding\n", $stderr);

        [$code, $stdout, $stderr] = $this->report(['bogus'], 'json', 'unsupported://do-not-connect');
        self::assertSame(2, $code);
        self::assertSame('', $stdout);
        $problem = json_decode($stderr, true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($problem);
        self::assertSame(422, $problem['status'] ?? null);
        self::assertSame([
            ['message' => 'unknown check "bogus"; the checks are passes, capacity, tombstones, receipt-counters', 'location' => '--only'],
        ], $problem['errors'] ?? null);
    }

    public function testReadOnlySnapshotRejectsWritesAndCanBeUsedAgain(): void
    {
        try {
            Database::inReadSnapshot($this->db, fn() => $this->insert('persons', ['id' => 1, 'given_name' => 'Fictional', 'family_name' => 'Person']));
            self::fail('A write unexpectedly succeeded inside a read-only snapshot');
        } catch (DatabaseError $error) {
            self::assertStringContainsString('read', strtolower($error->getMessage()));
        }
        self::assertSame([], $this->db->fetchFirstColumn('SELECT id FROM persons'));

        $store = new IntegrityStore($this->db);
        self::assertSame([], Database::inReadSnapshot($this->db, static fn() => $store->findings(self::CHECKS)));

        self::assertSame([2, '', "error: no database URL: set DATABASE_URL or --database-url\n"], $this->report([], 'text', ''));
        self::assertSame([2, '', "error: DATABASE_URL must name PostgreSQL, MySQL or MariaDB\n"], $this->report([], 'text', 'unsupported://fixture'));
        self::assertSame([2, '', "error: --as: studio-schedule db check acts as no account\n"], $this->report([], extra: ['--as=']));
        $this->db->executeStatement('DELETE FROM goose_db_version WHERE version_id = (SELECT max_version FROM (SELECT MAX(version_id) AS max_version FROM goose_db_version) m)');
        [$code, $out, $err] = $this->report([]);
        self::assertSame(1, $code);
        self::assertSame('', $out);
        self::assertStringContainsString('the database schema is not up to date', $err);
    }

    /**
     * @param list<string> $only
     * @param list<string> $extra
     * @return array{int,string,string}
     */
    private function report(array $only, string $format = 'text', ?string $dsn = null, array $extra = []): array
    {
        $input = fopen('php://temp', 'w+b');
        $stdout = fopen('php://temp', 'w+b');
        $stderr = fopen('php://temp', 'w+b');
        self::assertIsResource($input);
        self::assertIsResource($stdout);
        self::assertIsResource($stderr);
        try {
            $command = new Operator(dirname(__DIR__), $this->pgMigrations);
            $args = ['--database-url=' . ($dsn ?? $this->dsn), 'db', 'check', '-o', $format];
            foreach ($only as $check) {
                $args[] = "--only=$check";
            }
            $code = $command->run([...$args, ...$extra], $input, $stdout, $stderr, static fn(string $key): ?string => null);
            rewind($stdout);
            rewind($stderr);
            $out = stream_get_contents($stdout);
            $err = stream_get_contents($stderr);
            self::assertIsString($out);
            self::assertIsString($err);

            return [$code, $out, $err];
        } finally {
            fclose($input);
            fclose($stdout);
            fclose($stderr);
        }
    }

    /** @return list<array{check:string,entity:string,id:string,message:string}> */
    private static function expectedFindings(): array
    {
        $out = [
            self::finding('passes', 'pass', 60, '2 of its visits are used, but it paid for 1 bookings'),
            self::finding('passes', 'booking', 42, 'pass ' . self::id(60) . ' paid for it, but it costs no visit: the booking was canceled in time, and the school canceled lesson ' . self::id(31)),
            self::finding('capacity', 'lesson', 30, '3 active bookings for 1 places, and only 1 of them were booked beyond the capacity on purpose'),
        ];
        foreach ([
            [10, 'merged', 'booking', 40],
            [10, 'merged', 'open waiting-list entry', 50],
            [10, 'merged', 'membership of class', 27],
            [10, 'merged', 'guardian link to dependent', 11],
            [10, 'merged', 'guardian link from guardian', 9],
            [10, 'merged', 'pass %s it owns', 60],
            [10, 'merged', 'duplicate dismissal with person', 9],
            [10, 'merged', 'duplicate dismissal with person', 11],
            [10, 'merged', 'person %s merged into it', 14],
            [12, 'anonymized', 'booking', 46],
            [12, 'anonymized', 'pass %s it paid for', 60],
            [12, 'anonymized', 'staff record', 23],
            [12, 'anonymized', 'account', 16],
            [14, 'merged', 'booking', 44],
        ] as [$person, $kind, $description, $target]) {
            $ref = str_contains($description, '%s') ? sprintf($description, self::id($target)) : $description . ' ' . self::id($target);
            $out[] = self::finding('tombstones', 'person', $person, "the person is $kind, but $ref still refers to it");
        }
        $out[] = [
            'check' => 'receipt-counters', 'entity' => 'receipt-year', 'id' => '2026',
            'message' => 'the counter is at 0, but receipt number 3 was issued: the next receipt would repeat a number',
        ];

        return $out;
    }

    /** @return array{check:string,entity:string,id:string,message:string} */
    private static function finding(string $check, string $entity, int $id, string $message): array
    {
        return ['check' => $check, 'entity' => $entity, 'id' => self::id($id), 'message' => $message];
    }

    private function seedBrokenSchool(): void
    {
        foreach ([9, 10, 11, 12, 14, 15] as $id) {
            $this->insert('persons', ['id' => $id, 'given_name' => 'Fictional', 'family_name' => "Member $id"]);
        }
        $this->insert('accounts', ['id' => 17, 'person_id' => 10, 'email' => 'former@example.test', 'role' => 'member', 'disabled_at' => self::ANONYMIZED]);
        $this->insert('accounts', ['id' => 16, 'person_id' => 12, 'email' => 'current@example.test', 'role' => 'member']);
        $this->insert('locations', ['id' => 20, 'name' => 'Fictional Hall']);
        $this->insert('rooms', ['id' => 21, 'location_id' => 20, 'name' => 'Studio A', 'capacity' => 18, 'position' => 1]);
        $this->insert('course_types', ['id' => 22, 'name' => 'Fictional Ballet']);
        $this->insert('staff', ['id' => 23, 'person_id' => 12, 'display_name' => 'Fictional Teacher', 'kind' => 'instructor', 'active' => true]);
        $this->insert('terms', ['id' => 24, 'name' => 'Fictional Term', 'starts_on' => '2026-11-01', 'ends_on' => '2026-11-30']);
        $this->insert('classes', [
            'id' => 27, 'term_id' => 24, 'course_type_id' => 22, 'room_id' => 21, 'instructor_id' => 23,
            'weekday' => 0, 'start_time' => '10:00:00', 'duration' => $this->postgres ? '1 hour' : 3600000000, 'capacity' => 8,
        ]);
        $this->insert('class_members', ['class_id' => 27, 'person_id' => 10]);
        foreach ([[10, 11], [9, 10]] as [$guardian, $dependent]) {
            $this->insert('person_guardians', ['guardian_id' => $guardian, 'dependent_id' => $dependent]);
            $this->insert('person_duplicate_dismissals', ['person_a' => min($guardian, $dependent), 'person_b' => max($guardian, $dependent), 'dismissed_at' => self::CREATED, 'dismissed_by' => 17]);
        }

        foreach ([[30, self::LATER, false], [31, self::LATER, true], [32, self::CREATED, false], [33, self::LATER, false]] as [$id, $start, $canceled]) {
            $this->insert('lessons', [
                'id' => $id, 'course_type_id' => 22, 'room_id' => 21, 'instructor_id' => 23,
                'kind' => 'regular', 'visibility' => 'public', 'capacity' => 1, 'status' => $canceled ? 'canceled' : 'scheduled',
                'starts_at' => $start, 'ends_at' => str_replace('12:00:00', '13:00:00', $start),
                'canceled_at' => $canceled ? self::ANONYMIZED : null,
            ]);
        }
        foreach ([
            [40, 30, 10, 'booked', null],
            [41, 30, 11, 'booked', null],
            [42, 31, 12, 'canceled', self::ANONYMIZED],
            [43, 32, 12, 'booked', null],
            [44, 30, 14, 'booked', null],
            [45, 32, 15, 'booked', null],
            [46, 33, 12, 'booked', null],
        ] as [$id, $lesson, $person, $status, $canceledAt]) {
            $this->insert('bookings', [
                'id' => $id, 'lesson_id' => $lesson, 'person_id' => $person, 'booked_at' => self::CREATED,
                'status' => $status, 'trial' => false, 'canceled_at' => $canceledAt,
            ]);
        }
        foreach ([47 => 41, 48 => 41, 49 => 45] as $id => $booking) {
            $this->insert('booking_events', ['id' => $id, 'booking_id' => $booking, 'at' => self::CREATED, 'status' => 'booked', 'override' => true]);
        }
        foreach ([50 => 'waiting', 51 => 'withdrawn'] as $id => $status) {
            $this->insert('waitlist_entries', ['id' => $id, 'lesson_id' => 30, 'person_id' => 10, 'status' => $status, 'joined_at' => self::CREATED, 'stay_on_list' => false]);
        }

        $this->insert('pass_types', ['id' => 59, 'name' => 'Fictional Visits', 'kind' => 'visits', 'visits' => 5, 'validity_days' => 0, 'price' => 0, 'vat_rate_bp' => 0, 'active' => true, 'created_at' => self::CREATED]);
        $basePass = ['pass_type_id' => 59, 'kind' => 'visits', 'visits_total' => 5, 'valid_from' => '2026-11-01', 'status' => 'active', 'origin' => 'sale', 'price_paid' => 0];
        $this->insert('passes', ['id' => 60, 'person_id' => 10, 'payer_person_id' => 12, 'visits_remaining' => 3, 'sold_at' => self::LATER] + $basePass);
        $this->insert('passes', ['id' => 61, 'person_id' => 12, 'visits_remaining' => 5, 'sold_at' => self::CREATED] + $basePass);
        $this->insert('pass_redemptions', ['pass_id' => 60, 'booking_id' => 42, 'at' => self::CREATED]);

        $receipt = [
            'kind' => 'sale', 'locale' => 'en', 'issuer_name' => 'Fictional School', 'issuer_address' => 'Fictional Road',
            'issuer_tax_id' => 'FICTIONAL', 'tax_mode' => 'exempt', 'recipient_name' => 'Fictional Recipient',
            'payment_method' => 'cash', 'currency' => 'EUR', 'net' => 10, 'vat' => 0, 'gross' => 10,
        ];
        foreach ([2025 => 2, 2026 => 3] as $year => $sequence) {
            $this->insert('receipts', [
                'id' => $year, 'number' => "R-$year-$sequence", 'year' => $year, 'sequence' => $sequence,
                'issue_date' => "$year-01-01", 'issued_at' => "$year-01-01 12:00:00",
            ] + $receipt);
        }
        $this->insert('receipt_counters', ['year' => 2025, 'last' => 4]);
        $this->insert('receipt_counters', ['year' => 2027, 'last' => 1]);
        $this->db->executeStatement('UPDATE persons SET anonymized_at = ? WHERE id = ?', [self::ANONYMIZED, $this->encoded(12)]);
        $this->db->executeStatement('UPDATE persons SET merged_into = ?, anonymized_at = ? WHERE id = ?', [$this->encoded(11), self::ANONYMIZED, $this->encoded(10)]);
        $this->db->executeStatement('UPDATE persons SET merged_into = ? WHERE id = ?', [$this->encoded(10), $this->encoded(14)]);
    }

    /** @param array<string,string|int|bool|null> $fields */
    private function insert(string $table, array $fields): void
    {
        $types = [];
        foreach ($fields as $column => $value) {
            if (is_int($value) && ($column === 'id' || str_ends_with($column, '_id') || str_ends_with($column, '_by') || in_array($column, ['person_a', 'person_b'], true))) {
                $fields[$column] = $this->encoded($value);
                $types[$column] = $this->postgres ? ParameterType::STRING : ParameterType::BINARY;
            } else {
                $types[$column] = match (true) {
                    is_int($value) => ParameterType::INTEGER,
                    is_bool($value) => ParameterType::BOOLEAN,
                    $value === null => ParameterType::NULL,
                    default => ParameterType::STRING,
                };
            }
        }
        $this->db->insert($table, $fields, $types);
    }

    private function encoded(int $number): string
    {
        $id = self::id($number);
        return $this->postgres ? $id : pack('H*', str_replace('-', '', $id));
    }

    private static function id(int $number): string
    {
        return sprintf('018f0000-0000-7000-8000-%012d', $number);
    }
}
