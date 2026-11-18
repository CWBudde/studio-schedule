<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\DriverException;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Log\NullLogger;
use StudioSchedule\Service\BackgroundJobs;
use StudioSchedule\Service\JobResult;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\OutboxStore;
use StudioSchedule\Store\RetentionStore;
use StudioSchedule\Store\Schema;

final class RetentionJobsTest extends TestCase
{
    // This instant is August 31 in UTC. Six calendar months back is March 2
    // (February 31 normalized as in Go), even though the local date is September 1.
    private const string NOW = '2028-09-01T01:00:00.123456+13:00';
    private const string GUEST_BEFORE = '2027-08-31 12:00:00.123455';
    private const string GUEST_EDGE = '2027-08-31 12:00:00.123456';
    private const string OLD = '2020-01-01 00:00:00';
    private Connection $db;
    private bool $postgres;

    public function testJobsKeepTheExactBoundaryAndRetainBookingAndRegistrationHistory(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database provisioned by just php-test');
        }
        $this->db = Database::connect($dsn);
        $this->postgres = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        self::assertSame([], (new Schema($this->db))->relations(), 'The retention fixture needs an empty disposable database');
        try {
            $path = $this->postgres
                ? (getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations')
                : dirname(__DIR__) . '/migrations/mysql';
            (new Migrator($dsn))->up($path);
            $this->seedMail();
            $this->seedGuests();
            $this->seedHistory();
            $this->assertPurgesRollback();
            $clock = self::createStub(ClockInterface::class);
            $clock->method('now')->willReturn(new DateTimeImmutable(self::NOW));
            $jobs = new BackgroundJobs($this->db, $dsn, $clock, new NullLogger());

            self::assertSame(JobResult::Ok, $jobs->purgeMail());
            self::assertSame(JobResult::Ok, $jobs->purgePersonalData());
            self::assertNull($jobs->purgeMail());
            self::assertNull($jobs->purgePersonalData());
            $this->assertRemainingRows();

            // The same cutoff changes nothing, even when the store is called directly.
            self::assertSame(['bodies' => 0, 'deleted' => 0], (new OutboxStore($this->db))->purge(new DateTimeImmutable(self::NOW)));
            $counts = (new RetentionStore($this->db))->purge(new DateTimeImmutable(self::NOW));
            self::assertSame(0, array_sum($counts));
        } finally {
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
    }

    private function seedMail(): void
    {
        $this->insert('receipts', [
            'id' => 160, 'number' => 'SD-2028-0001', 'year' => 2028, 'sequence' => 1, 'kind' => 'sale',
            'issued_at' => '2028-03-03 10:00:00', 'issue_date' => '2028-03-03', 'locale' => 'en',
            'issuer_name' => 'Fixture School', 'issuer_address' => 'Fixture Street', 'issuer_tax_id' => '1234', 'tax_mode' => 'exempt',
            'recipient_name' => 'Fixture recipient', 'payment_method' => 'cash', 'currency' => 'EUR', 'net' => 100, 'vat' => 0, 'gross' => 100,
        ]);
        $this->insert('receipt_pdfs', ['receipt_id' => 160, 'pdf' => '%PDF-fixture']);
        foreach ([
            201 => ['2028-03-05 12:00:00', '2028-08-01 12:00:00.123455', null],
            202 => ['2028-03-05 12:00:00', '2028-08-01 12:00:00.123456', null],
            203 => ['2028-03-04 12:00:00.123456', null, '2028-08-31 12:00:00'],
            204 => ['2028-03-04 12:00:00.123455', null, '2028-08-31 12:00:00'],
            205 => ['2028-03-04 12:00:00.123455', null, null],
            206 => ['2028-03-05 12:00:00', null, null],
        ] as $id => [$created, $sent, $due]) {
            $this->insert('outbox_mails', [
                'id' => $id, 'recipient' => "person-$id@example.test", 'subject' => "Fixture $id", 'body_text' => 'Body',
                'body_html' => '<p>Body</p>', 'created_at' => $created, 'sent_at' => $sent, 'next_attempt_at' => $due, 'attempts' => 1,
                'last_error' => $sent === null && $due === null ? 'Fixture failure' : null,
                'receipt_id' => $id === 205 ? 160 : null,
            ]);
        }
    }

    private function seedGuests(): void
    {
        $this->insert('persons', ['id' => 1, 'given_name' => 'Fixture', 'family_name' => 'Member']);
        $this->insert('accounts', ['id' => 2, 'person_id' => 1, 'email' => 'member@example.test', 'role' => 'member']);
        $this->insert('locations', ['id' => 3, 'name' => 'Fixture location']);
        $this->insert('rooms', ['id' => 4, 'location_id' => 3, 'name' => 'Room', 'capacity' => 10, 'position' => 1]);
        $this->insert('course_types', ['id' => 5, 'name' => 'Ballet']);
        $this->insert('staff', ['id' => 6, 'display_name' => 'Fixture teacher', 'kind' => 'instructor', 'active' => true]);

        foreach (['old', 'mixed', 'no-sessions'] as $index => $tag) {
            $this->insert('events', [
                'id' => 20 + $index, 'slug' => $tag, 'title' => $tag, 'course_type_id' => 5,
                'capacity' => 10, 'price' => 0, 'vat_rate_bp' => 0, 'status' => 'published',
                'created_at' => self::OLD, 'updated_at' => self::OLD,
            ]);
            $this->insert('event_registrations', [
                'id' => 60 + $index, 'event_id' => 20 + $index, 'guest_name' => $tag, 'guest_email' => "$tag@example.test",
                'status' => 'registered', 'payment' => 'unpaid', 'price' => 0, 'vat_rate_bp' => 0,
                'created_at' => self::OLD, 'updated_at' => self::OLD,
            ]);
        }

        foreach ([
            30 => [null, self::GUEST_BEFORE, 'scheduled'],
            31 => [null, self::GUEST_EDGE, 'scheduled'],
            32 => [20, self::GUEST_BEFORE, 'scheduled'],
            33 => [21, self::GUEST_BEFORE, 'scheduled'],
            34 => [21, self::GUEST_EDGE, 'canceled'],
        ] as $id => [$event, $ends, $status]) {
            $this->insert('lessons', [
                'id' => $id, 'event_id' => $event, 'course_type_id' => 5, 'room_id' => 4, 'instructor_id' => 6,
                'kind' => $event === null ? 'regular' : 'workshop', 'visibility' => 'public', 'status' => $status, 'capacity' => 10,
                'starts_at' => sprintf('2027-08-31 10:%02d:00', $id), 'ends_at' => $ends,
            ]);
        }
        foreach ([40 => [30, 'old'], 41 => [31, 'edge'], 42 => [30, null], 43 => [30, 'Guest'], 44 => [30, 'Guest ']] as $id => [$lesson, $guest]) {
            $this->insert('bookings', [
                'id' => $id, 'lesson_id' => $lesson, 'person_id' => $guest === null ? 1 : null,
                'guest_name' => $guest, 'guest_email' => $guest === null || $id === 44 ? null : "person-$id@example.test",
                'status' => 'attended', 'booked_at' => self::OLD, 'trial' => true,
                'trial_note' => $id === 44 ? null : "Note $id", 'source' => 'staff',
            ]);
        }
    }

    private function seedHistory(): void
    {
        foreach (['123455', '123456'] as $index => $fraction) {
            $this->insert('audit_events', [
                'id' => 100 + $index, 'actor_id' => 2, 'target_id' => 2, 'target_kind' => 'account',
                'action' => 'impersonation-ended', 'occurred_at' => "2025-08-31 12:00:00.$fraction",
            ]);
            $this->insert('import_runs', [
                'id' => 110 + $index, 'file_name' => "fixture-$index.csv", 'started_at' => "2027-08-31 12:00:00.$fraction",
                'finished_at' => '2027-08-31 13:00:00', 'rows_total' => 0, 'created' => 0, 'updated' => 0,
                'unchanged' => 0, 'failed' => 0, 'guardians_created' => 0, 'guardians_updated' => 0, 'links_added' => 0,
            ]);
            $this->insert('job_runs', [
                'id' => 120 + $index, 'job' => "fixture-$index", 'started_at' => "2028-08-01 12:00:00.$fraction",
                'ended_at' => '2028-08-01 13:00:00', 'result' => 'ok',
            ]);
            $this->insert('push_subscriptions', [
                'id' => 130 + $index, 'account_id' => 2, 'endpoint' => "https://push.example.test/fixture-$index",
                'p256dh' => str_repeat('x', 65), 'auth' => str_repeat('y', 16), 'created_at' => self::OLD,
                'last_used_at' => "2028-03-02 12:00:00.$fraction",
            ]);
        }
        foreach ([
            140 => [131, '2028-08-24 12:00:00.123455'],
            141 => [131, '2028-08-24 12:00:00.123456'],
            142 => [130, '2028-08-31 12:00:00'],
        ] as $id => [$subscription, $created]) {
            $this->insert('push_outbox', [
                'id' => $id, 'subscription_id' => $subscription, 'created_at' => $created,
                'expires_at' => '2028-09-02 12:00:00', 'next_attempt_at' => $created, 'payload' => '{}',
            ]);
        }
    }

    private function assertRemainingRows(): void
    {
        self::assertSame([
            ['Fixture 201', null, null], ['Fixture 202', 'Body', '<p>Body</p>'],
            ['Fixture 203', 'Body', '<p>Body</p>'], ['Fixture 206', 'Body', '<p>Body</p>'],
        ], $this->db->fetchAllNumeric('SELECT subject, body_text, body_html FROM outbox_mails ORDER BY id'));
        self::assertSame(['SD-2028-0001'], $this->db->fetchFirstColumn('SELECT number FROM receipts'));
        $hex = $this->postgres ? "encode(pdf, 'hex')" : 'LOWER(HEX(pdf))';
        self::assertSame([bin2hex('%PDF-fixture')], $this->db->fetchFirstColumn("SELECT $hex FROM receipt_pdfs"));
        self::assertSame([
            ['Guest', null, null, 'attended'], ['edge', 'person-41@example.test', 'Note 41', 'attended'],
            [null, null, 'Note 42', 'attended'], ['Guest', null, null, 'attended'], ['Guest', null, null, 'attended'],
        ], $this->db->fetchAllNumeric('SELECT guest_name, guest_email, trial_note, status FROM bookings ORDER BY id'));
        self::assertSame([
            ['Guest', null, 'registered'], ['mixed', 'mixed@example.test', 'registered'],
            ['no-sessions', 'no-sessions@example.test', 'registered'],
        ], $this->db->fetchAllNumeric('SELECT guest_name, guest_email, status FROM event_registrations ORDER BY id'));
        self::assertSame([$this->id(101)], $this->db->fetchFirstColumn('SELECT id FROM audit_events'));
        self::assertSame(['fixture-1.csv'], $this->db->fetchFirstColumn('SELECT file_name FROM import_runs'));
        self::assertSame(['fixture-1', 'purge-mail', 'purge-personal-data'], $this->db->fetchFirstColumn('SELECT job FROM job_runs ORDER BY job'));
        self::assertSame(['https://push.example.test/fixture-1'], $this->db->fetchFirstColumn('SELECT endpoint FROM push_subscriptions'));
        self::assertSame([$this->id(141)], $this->db->fetchFirstColumn('SELECT id FROM push_outbox'));
    }

    private function assertPurgesRollback(): void
    {
        $now = new DateTimeImmutable(self::NOW);
        $this->withFailedDelete('outbox_mails', function () use ($now): void {
            (new OutboxStore($this->db))->purge($now);
        });
        self::assertSame('Body', $this->db->fetchOne('SELECT body_text FROM outbox_mails WHERE id = ?', [$this->id(201)]));

        $this->withFailedDelete('import_runs', function () use ($now): void {
            (new RetentionStore($this->db))->purge($now);
        });
        self::assertSame('old', $this->db->fetchOne('SELECT guest_name FROM bookings WHERE id = ?', [$this->id(40)]));
        self::assertSame('old', $this->db->fetchOne('SELECT guest_name FROM event_registrations WHERE id = ?', [$this->id(60)]));
        self::assertSame([$this->id(100), $this->id(101)], $this->db->fetchFirstColumn('SELECT id FROM audit_events ORDER BY id'));
    }

    /** @param 'outbox_mails'|'import_runs' $table @param callable():void $purge */
    private function withFailedDelete(string $table, callable $purge): void
    {
        if ($this->postgres) {
            $this->db->executeStatement(<<<'SQL'
                CREATE FUNCTION fixture_refuse_delete() RETURNS trigger LANGUAGE plpgsql AS $$
                BEGIN RAISE EXCEPTION 'Fixture refuses the delete'; END;
                $$
                SQL);
            $this->db->executeStatement("CREATE TRIGGER fixture_refuse_delete BEFORE DELETE ON $table FOR EACH ROW EXECUTE FUNCTION fixture_refuse_delete()");
        } else {
            $this->db->executeStatement("CREATE TRIGGER fixture_refuse_delete BEFORE DELETE ON $table FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Fixture refuses the delete'");
        }
        try {
            $purge();
            self::fail('The fixture should prevent the purge from committing');
        } catch (DriverException $error) {
            self::assertStringContainsString('Fixture refuses the delete', $error->getMessage());
        } finally {
            if ($this->postgres) {
                $this->db->executeStatement("DROP TRIGGER fixture_refuse_delete ON $table");
                $this->db->executeStatement('DROP FUNCTION fixture_refuse_delete()');
            } else {
                $this->db->executeStatement('DROP TRIGGER fixture_refuse_delete');
            }
        }
    }

    /** @param array<string,string|int|bool|null> $fields */
    private function insert(string $table, array $fields): void
    {
        $types = [];
        foreach ($fields as $name => $value) {
            $types[$name] = match (true) {
                is_bool($value) => ParameterType::BOOLEAN,
                $value === null => ParameterType::NULL,
                is_int($value) => ParameterType::INTEGER,
                default => ParameterType::STRING,
            };
            if (is_int($value) && ($name === 'id' || str_ends_with($name, '_id'))) {
                $fields[$name] = $this->id($value);
                $types[$name] = $this->postgres ? ParameterType::STRING : ParameterType::BINARY;
            }
        }
        $this->db->insert($table, $fields, $types);
    }

    private function id(int $number): string
    {
        $uuid = sprintf('018f0000-0000-7000-8000-%012x', $number);

        return $this->postgres ? $uuid : pack('H*', str_replace('-', '', $uuid));
    }
}
