<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\DriverException;
use Doctrine\DBAL\Exception\LockWaitTimeoutException;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Http\Message\ResponseInterface;
use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\Psr7\Factory\StreamFactory;
use StudioSchedule\Bootstrap;
use StudioSchedule\Receipt\AttachmentStore;
use StudioSchedule\Receipt\Attachments;
use StudioSchedule\Store\CatalogStore;
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

    public function testPrivacyExportProjectsHistoryAndRedactsADependentsAccount(): void
    {
        $this->request('GET', '/my/export?person=' . self::id(1), user: null, status: 401, mount: '');
        $this->request('GET', '/my/export', status: 422, mount: '');
        $this->request('GET', '/my/export?person=invalid', status: 422, mount: '');
        $this->request('GET', '/my/export?person=' . self::id(1), user: 'outsider', status: 404, mount: '');
        $this->request('GET', '/admin/persons/' . self::id(1) . '/export', status: 403, mount: '');
        $this->account(6, 'office');
        $this->session(6, 'office', 36);
        $this->seedExportHistory();

        $result = $this->request('GET', '/my/export?person=' . self::id(1), user: 'acting-parent', mount: '');
        self::assertSame('studio-schedule.person-export', $result['format']);
        self::assertSame(1, $result['version']);
        self::assertSame(self::NOW, $result['exportedAt']);
        self::assertIsArray($result['person']);
        self::assertSame('office-only note', $result['person']['notes']);
        self::assertSame('M-123', $result['person']['memberNo']);
        self::assertSame('private-contact@example.test', $result['person']['email']);
        self::assertSame([], $result['guardians']);
        self::assertIsArray($result['dependents']);
        self::assertSame(['id' => self::id(3), 'givenName' => 'Kid', 'familyName' => 'Alpha'], $result['dependents'][0]);
        self::assertSame([['id' => self::id(11), 'givenName' => 'Old Profile', 'familyName' => 'Example']], $result['mergedPersons']);
        self::assertIsArray($result['account']);
        self::assertSame('person-1@example.test', $result['account']['email']);
        self::assertEquals([['id' => self::id(63), 'scope' => 'schedule', 'locationId' => self::id(50), 'createdAt' => self::NOW]], $result['calendarFeeds']);
        self::assertEquals([['id' => self::id(65), 'deviceLabel' => 'Fixture Browser', 'createdAt' => self::NOW, 'lastUsedAt' => self::NOW]], $result['pushDevices']);
        self::assertSame([['id' => self::id(66), 'title' => 'Fixture announcement', 'publishAt' => self::NOW]], $result['dismissedAnnouncements']);

        $membership = [
            'classId' => self::id(54), 'termId' => self::id(53), 'termName' => 'Autumn', 'courseTypeName' => 'Ballet',
            'weekday' => 4, 'startTime' => '15:00', 'retired' => false, 'since' => '2026-11-01',
        ];
        self::assertEquals([$membership, ['classId' => self::id(55)] + $membership], $result['classMemberships']);
        $lesson = ['id' => self::id(56), 'startsAt' => '2026-11-18T15:00:00.123456Z', 'endsAt' => '2026-11-18T16:00:00.123456Z', 'courseType' => 'Ballet'];
        self::assertEquals([[
            'id' => self::id(57), 'lesson' => $lesson, 'status' => 'canceled', 'source' => 'self',
            'trial' => true, 'trialNote' => 'Trial note', 'bookedAt' => '2026-11-17T12:00:00Z', 'canceledAt' => self::NOW, 'late' => true,
        ]], $result['bookings']);
        self::assertEquals([[
            'id' => self::id(58), 'lesson' => $lesson, 'status' => 'withdrawn',
            'joinedAt' => '2026-11-17T12:00:00Z', 'invitedAt' => '2026-11-18T12:00:00Z',
        ]], $result['waitlistEntries']);
        self::assertEquals([[
            'id' => self::id(61), 'title' => 'Fixture workshop', 'status' => 'canceled', 'payment' => 'refunded',
            'price' => 2500, 'receiptNumber' => 'R-2026-71', 'cancellationReceiptNumber' => 'R-2026-72',
            'createdAt' => '2026-11-17T12:00:00Z', 'canceledAt' => self::NOW,
        ]], $result['eventRegistrations']);
        self::assertEquals([[
            'id' => self::id(60), 'type' => 'Two visits', 'kind' => 'visits', 'visitsTotal' => 2, 'visitsRemaining' => 1,
            'validFrom' => '2026-11-01', 'status' => 'active', 'soldAt' => '2026-11-01T12:00:00Z', 'pricePaid' => 0,
            'redemptions' => [['bookingId' => self::id(57), 'at' => '2026-11-17T12:00:00Z']],
        ]], $result['passes']);
        self::assertIsArray($result['receipts']);
        self::assertSame(['R-2026-72', 'R-2026-71'], array_column($result['receipts'], 'number'));
        self::assertSame([-2500, 2500], array_column($result['receipts'], 'gross'));
        self::assertSame([false, true], array_column($result['receipts'], 'canceled'));
        $document = json_encode($result, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES);
        foreach (['fixture-private-hash', 'https://push.example.test/secret', 'token_hash', '%PDF-fixture', 'passwordHash', 'inviteToken'] as $secret) {
            self::assertStringNotContainsString($secret, $document);
        }

        $dependent = $this->request('GET', '/my/export?person=' . self::id(2), mount: '');
        self::assertArrayNotHasKey('account', $dependent);
        foreach (['calendarFeeds', 'pushDevices', 'dismissedAnnouncements'] as $key) {
            self::assertSame([], $dependent[$key]);
        }
        self::assertSame($result, $this->request('GET', '/admin/persons/' . self::id(1) . '/export', user: 'office', mount: ''));
        $childOfOffice = $this->request('GET', '/admin/persons/' . self::id(2) . '/export', user: 'office', mount: '');
        self::assertIsArray($childOfOffice['account']);
        self::assertSame('person-2@example.test', $childOfOffice['account']['email']);
        foreach (['calendarFeeds', 'pushDevices', 'dismissedAnnouncements'] as $key) {
            self::assertIsArray($childOfOffice[$key]);
            self::assertCount(1, $childOfOffice[$key]);
        }
        $deleted = $this->request('GET', '/admin/persons/' . self::id(10) . '/export', user: 'office', mount: '');
        self::assertIsArray($deleted['person']);
        self::assertSame('2026-11-17T10:00:00Z', $deleted['person']['anonymizedAt']);
        $merged = $this->request('GET', '/admin/persons/' . self::id(11) . '/export', user: 'office', mount: '');
        self::assertIsArray($merged['person']);
        self::assertSame(self::id(1), $merged['person']['mergedIntoId']);
    }

    public function testReceiptPdfUsesTheCurrentPayerAndEventFamilyAccess(): void
    {
        $this->account(4, 'office');
        $this->account(6, 'member');
        $this->seedExportHistory();
        $this->db->executeStatement('UPDATE event_registrations SET person_id = ? WHERE id = ?', [$this->uuid(3), $this->uuid(61)]);
        $this->session(4, 'office', 34);
        $this->session(6, 'guardian', 36);
        $this->session(7, 'acting-outsider', 38, 9);
        $this->insert('receipt_lines', [
            'receipt_id' => 72, 'position' => 1, 'description' => 'Workshop fee refund',
            'quantity' => 1, 'unit_price' => -2500, 'vat_rate_bp' => 0, 'net' => -2500, 'vat' => 0, 'gross' => -2500,
        ]);

        $sale = '/receipts/' . self::id(71) . '/pdf';
        $cancellation = '/receipts/' . self::id(72) . '/pdf';
        $this->request('GET', '/my' . $sale, user: null, status: 401, mount: '');
        $this->request('GET', '/admin' . $sale, user: 'parent', status: 403, mount: '');
        $this->request('GET', '/my' . $sale, user: 'outsider', status: 404, mount: '');
        $this->request('GET', '/my' . $sale, user: 'acting-outsider', status: 404, mount: '');
        $this->request('GET', '/admin' . $sale, user: 'acting-outsider', status: 403, mount: '');
        $this->request('GET', '/my' . $cancellation, user: 'outsider', status: 404, mount: '');
        $this->request('GET', '/admin/receipts/' . self::id(73) . '/pdf', user: 'office', status: 404, mount: '');
        $this->request('GET', '/my/receipts/invalid/pdf', status: 422, mount: '');
        self::assertCount(1, $this->db->fetchFirstColumn('SELECT receipt_id FROM receipt_pdfs'));

        foreach ([
            ['/admin' . $sale, 'office'],
            ['/my' . $sale, 'guardian'],
            ['/my' . $sale, 'acting-parent'],
        ] as [$path, $user]) {
            $response = $this->response('GET', $path, null, $user, '');
            self::assertSame(200, $response->getStatusCode());
            self::assertSame('application/pdf', $response->getHeaderLine('Content-Type'));
            self::assertSame('attachment; filename="R-2026-71.pdf"', $response->getHeaderLine('Content-Disposition'));
            self::assertSame('private, no-store', $response->getHeaderLine('Cache-Control'));
            self::assertSame('%PDF-fixture', (string) $response->getBody());
        }

        $response = $this->response('GET', '/my' . $cancellation, null, 'guardian', '');
        self::assertSame(200, $response->getStatusCode());
        self::assertSame('attachment; filename="R-2026-72.pdf"', $response->getHeaderLine('Content-Disposition'));
        $pdf = (string) $response->getBody();
        self::assertStringStartsWith('%PDF-', $pdf);
        self::assertCount(2, $this->db->fetchFirstColumn('SELECT receipt_id FROM receipt_pdfs'));

        $this->db->executeStatement('DELETE FROM person_guardians WHERE dependent_id = ?', [$this->uuid(3)]);
        $this->request('GET', '/my' . $cancellation, user: 'guardian', status: 404, mount: '');
        $response = $this->response('GET', '/my' . $cancellation, null, 'parent', '');
        self::assertSame(200, $response->getStatusCode()); // This payer's old profile was merged into theirs.
        self::assertSame($pdf, (string) $response->getBody());
        self::assertSame(405, $this->response('HEAD', '/my' . $sale, null, 'parent', '')->getStatusCode());
    }

    public function testReceiptPdfRechecksFamilyWhenAnotherRequestCachesItWhileRendering(): void
    {
        $this->account(6, 'member');
        $this->seedExportHistory();
        $this->db->executeStatement('UPDATE event_registrations SET person_id = ? WHERE id = ?', [$this->uuid(3), $this->uuid(61)]);

        $store = new AttachmentStore($this->db);
        $receipt = self::id(72);
        $attachments = new Attachments($store, function () use ($store, $receipt): string {
            $this->db->executeStatement('DELETE FROM person_guardians WHERE guardian_id = ?', [$this->uuid(6)]);
            $store->put($receipt, '%PDF-original');

            return '%PDF-rendered';
        });
        self::assertNull($attachments->findForPerson($receipt, self::id(6)));
        self::assertSame(['filename' => 'R-2026-72.pdf', 'pdf' => '%PDF-original'], $store->find($receipt));
    }

    public function testReceiptReadsKeepOfficeSearchAndMemberOwnershipSeparate(): void
    {
        $this->account(6, 'office');
        $this->session(6, 'office', 36);
        $this->seedExportHistory();
        $this->insert('receipt_counters', ['year' => 2025, 'last' => 1]);
        $this->insert('receipt_counters', ['year' => 2026, 'last' => 73]);
        $this->insert('receipts', [
            'id' => 73, 'number' => 'R-2026-73', 'year' => 2026, 'sequence' => 73, 'kind' => 'sale',
            'issued_at' => '2026-11-18 23:30:00.123456', 'issue_date' => '2026-11-19', 'locale' => 'en',
            'issuer_name' => 'Studio', 'issuer_address' => 'Road', 'issuer_tax_id' => '123',
            'tax_mode' => 'exempt', 'recipient_name' => 'DÉMO %_payer', 'recipient_email' => 'guest@example.test',
            'payment_method' => 'card', 'currency' => 'EUR', 'net' => 400, 'vat' => 0, 'gross' => 400,
        ]);
        $this->insert('receipt_lines', [
            'receipt_id' => 72, 'position' => 1, 'description' => 'Workshop fee refund',
            'quantity' => 1, 'unit_price' => -2500, 'vat_rate_bp' => 0, 'net' => -2500, 'vat' => 0, 'gross' => -2500,
        ]);

        $search = '/admin/receipts/search';
        $this->request('POST', $search, [], user: null, status: 401, mount: '');
        $this->request('POST', $search, [], status: 403, mount: '');
        $this->request('GET', '/my/receipts', user: null, status: 401, mount: '');
        self::assertSame(['items' => []], $this->request('GET', '/my/receipts', user: 'outsider', mount: ''));
        $mine = $this->request('GET', '/my/receipts', user: 'acting-parent', mount: '');
        self::assertIsArray($mine['items']);
        self::assertSame(['R-2026-72', 'R-2026-71'], array_column($mine['items'], 'number'));
        [$cancellation, $sale] = $mine['items'];
        self::assertIsArray($cancellation);
        self::assertIsArray($sale);
        self::assertSame(['id' => self::id(11), 'name' => 'Old Profile Example'], $sale['payer']);
        self::assertSame(['id' => self::id(72), 'number' => 'R-2026-72', 'issueDate' => '2026-11-19'], $sale['canceledBy']);
        self::assertSame(['id' => self::id(71), 'number' => 'R-2026-71', 'issueDate' => '2026-11-19'], $cancellation['cancels']);
        self::assertSame(self::id(62), $sale['eventId']);
        self::assertSame('Fixture workshop', $cancellation['eventTitle']);
        self::assertFalse($sale['cancelable']);
        self::assertFalse($cancellation['cancelable']);
        self::assertEquals([[
            'position' => 1, 'description' => 'Workshop fee refund', 'quantity' => 1, 'unitPrice' => -2500,
            'vatRate' => 0, 'net' => -2500, 'vat' => 0, 'gross' => -2500,
        ]], $cancellation['lines']);
        self::assertSame($sale, $this->request('GET', '/admin/receipts/' . self::id(71), user: 'office', mount: ''));
        $this->request('GET', '/admin/receipts/' . self::id(74), user: 'office', status: 404, mount: '');
        $this->request('GET', '/admin/receipts/invalid', user: 'office', status: 422, mount: '');

        $result = $this->request('POST', $search, ['query' => ' r-2026 ', 'offset' => 1, 'limit' => 1], user: 'office', mount: '');
        self::assertSame(['items' => [$cancellation], 'total' => 3, 'years' => [2026, 2025]], $result);
        $result = $this->request('POST', $search, ['year' => 2026, 'query' => 'dÉmo %_'], user: 'office', mount: '');
        self::assertSame(1, $result['total']);
        self::assertIsArray($result['items']);
        [$guest] = $result['items'];
        self::assertIsArray($guest);
        self::assertSame('2026-11-18T23:30:00.123456Z', $guest['issuedAt']);
        self::assertSame('guest@example.test', $guest['recipientEmail']);
        self::assertTrue($guest['cancelable']);
        self::assertSame([], $guest['lines']);
        self::assertArrayNotHasKey('payer', $guest);
        foreach ([['year' => 2025], ['query' => 'démo'], ['query' => 'R%2026']] as $filter) {
            self::assertSame(['items' => [], 'total' => 0, 'years' => [2026, 2025]], $this->request('POST', $search, $filter, user: 'office', mount: ''));
        }
        foreach ([['limit' => 101], ['offset' => -1], ['year' => 10000], ['query' => str_repeat('ä', 101)]] as $invalid) {
            $this->request('POST', $search, $invalid, user: 'office', status: 422, mount: '');
        }

        $stamp = '2026-11-18 23:30:00';
        $detail = '/admin/receipts/' . self::id(73);
        $this->insert('event_registrations', [
            'id' => 74, 'event_id' => 62, 'guest_name' => 'Fixture guest', 'status' => 'registered', 'payment' => 'paid',
            'price' => 400, 'vat_rate_bp' => 0, 'receipt_id' => 73, 'created_at' => $stamp, 'updated_at' => $stamp,
        ]);
        self::assertFalse($this->request('GET', $detail, user: 'office', mount: '')['cancelable']);

        $this->db->executeStatement("UPDATE event_registrations SET status = 'canceled', canceled_at = ? WHERE id = ?", [$stamp, $this->uuid(74)]);
        self::assertTrue($this->request('GET', $detail, user: 'office', mount: '')['cancelable']);
        $this->insert('lessons', [
            'id' => 75, 'event_id' => 62, 'course_type_id' => 52, 'room_id' => 51, 'instructor_id' => 21,
            'starts_at' => $stamp, 'ends_at' => '2026-11-19 00:30:00', 'kind' => 'workshop', 'visibility' => 'public',
            'status' => 'scheduled', 'capacity' => 7,
        ]);
        self::assertFalse($this->request('GET', $detail, user: 'office', mount: '')['cancelable']);
        $this->db->executeStatement("UPDATE lessons SET status = 'canceled' WHERE id = ?", [$this->uuid(75)]);
        self::assertTrue($this->request('GET', $detail, user: 'office', mount: '')['cancelable']);

        $this->db->executeStatement('DELETE FROM event_registrations WHERE id = ?', [$this->uuid(74)]);
        $this->db->executeStatement("UPDATE passes SET origin = 'sale', receipt_id = ?, price_paid = 400 WHERE id = ?", [$this->uuid(73), $this->uuid(60)]);
        $used = $this->request('GET', $detail, user: 'office', mount: '');
        self::assertSame(self::id(60), $used['passId']);
        self::assertFalse($used['cancelable']);
        $this->db->executeStatement('DELETE FROM pass_redemptions WHERE pass_id = ?', [$this->uuid(60)]);
        self::assertTrue($this->request('GET', $detail, user: 'office', mount: '')['cancelable']);
    }

    public function testOfficeCanManagePassTypesWithoutChangingExistingPasses(): void
    {
        $this->account(6, 'office');
        $this->session(6, 'office', 36);
        $this->seedExportHistory();
        $catalog = '/admin/pass-types';
        $used = $catalog . '/' . self::id(59);
        $input = ['name' => ' Annual YÉAR ', 'kind' => 'time', 'validityDays' => 365, 'price' => 3000, 'vatRate' => 1900, 'active' => true];

        $this->request('GET', $catalog, user: null, status: 401, mount: '');
        $this->request('POST', $catalog, $input, status: 403, mount: '');
        $first = $this->request('POST', $catalog, $input, user: 'office', status: 201, mount: '');
        self::assertSame('Annual YÉAR', $first['name']);
        self::assertSame(self::NOW, $first['createdAt']);
        self::assertArrayNotHasKey('visits', $first);
        self::assertIsString($first['id']);
        $href = $catalog . '/' . $first['id'];

        $duplicate = $this->request('POST', $catalog, ['name' => 'annual yÉar'] + $input, user: 'office', status: 409, mount: '');
        self::assertSame('duplicate', $duplicate['reason']);
        $second = $this->request('POST', $catalog, ['name' => 'Annual YéAR'] + $input, user: 'office', status: 201, mount: '');
        self::assertSame('Annual YéAR', $second['name']);
        self::assertIsString($second['id']);
        foreach ([
            ['validityDays' => 0], ['visits' => 1], ['name' => '   '], ['kind' => 'visits'], ['price' => -1],
        ] as $invalid) {
            $this->request('POST', $catalog, $invalid + $input, user: 'office', status: 422, mount: '');
        }
        $missingActive = $input;
        unset($missingActive['active']);
        $this->request('POST', $catalog, $missingActive, user: 'office', status: 422, mount: '');

        $changed = $this->request('PUT', $used, [
            'name' => '  Twelve visits ', 'kind' => 'visits', 'visits' => 12, 'validityDays' => 90, 'price' => 5600, 'vatRate' => 0, 'active' => true,
        ], user: 'office', mount: '');
        self::assertSame('Twelve visits', $changed['name']);
        self::assertSame('2026-11-17T12:00:00Z', $changed['createdAt']);
        self::assertSame(12, $changed['visits']);
        $pass = $this->db->fetchAssociative('SELECT visits_total, visits_remaining, valid_until, price_paid FROM passes');
        self::assertEquals(['visits_total' => 2, 'visits_remaining' => 1, 'valid_until' => null, 'price_paid' => 0], $pass);
        $inUse = $this->request('DELETE', $used, user: 'office', status: 409, mount: '');
        self::assertSame('in-use', $inUse['reason']);
        $types = $this->request('GET', $catalog, user: 'office', mount: '');
        self::assertIsArray($types['items']);
        self::assertSame(['Annual YÉAR', 'Annual YéAR', 'Twelve visits'], array_column($types['items'], 'name'));
        self::assertSame([true, true, true], array_column($types['items'], 'active'));

        $retired = $this->request('POST', $used . '/retire', user: 'office', mount: '');
        self::assertSame(array_replace($changed, ['active' => false]), $retired);
        self::assertSame($retired, $this->request('POST', $used . '/retire', user: 'office', mount: ''));
        self::assertSame(204, $this->response('DELETE', $href, null, 'office', '')->getStatusCode());
        $this->request('POST', $href . '/retire', user: 'office', status: 404, mount: '');
        $this->request('DELETE', $href, user: 'office', status: 404, mount: '');
        $this->request('PUT', $href, $input, user: 'office', status: 404, mount: '');
        $this->request('DELETE', $catalog . '/invalid', user: 'office', status: 422, mount: '');
        $types = $this->request('GET', $catalog, user: 'office', mount: '');
        self::assertIsArray($types['items']);
        self::assertSame(['Annual YéAR', 'Twelve visits'], array_column($types['items'], 'name'));
        self::assertSame([true, false], array_column($types['items'], 'active'));

        if ($this->postgres) {
            // PDO returns timestamps as text even when a PostgreSQL role uses a non-ISO DateStyle.
            $this->db->executeStatement("SET DateStyle TO 'SQL, DMY'");
            try {
                $store = new CatalogStore($this->db);
                self::assertSame('2026-11-17T12:00:00Z', $store->passTypes()[1]['createdAt']);
                self::assertSame($retired, $store->retirePassType(self::id(59)));
                $saved = $store->savePassType(self::id(59), static fn(): array => [
                    'name' => 'Twelve visits', 'kind' => 'visits', 'visits' => 12,
                    'validityDays' => 90, 'price' => 5600, 'vatRate' => 0, 'active' => false,
                ], new DateTimeImmutable(self::NOW));
                self::assertSame($retired, $saved);
            } finally {
                $this->db->executeStatement('RESET DateStyle');
            }
        }
    }

    private function seedExportHistory(): void
    {
        $stamp = '2026-11-18 23:30:00';
        $earlier = '2026-11-17 12:00:00';
        $this->db->executeStatement("UPDATE persons SET member_no = 'M-123' WHERE id = ?", [$this->uuid(1)]);
        $this->insert('persons', ['id' => 11, 'given_name' => 'Old Profile', 'family_name' => 'Example', 'merged_into' => 1]);
        $this->insert('locations', ['id' => 50, 'name' => 'Fixture Hall']);
        $this->insert('rooms', ['id' => 51, 'location_id' => 50, 'name' => 'Room', 'capacity' => 20, 'position' => 1]);
        $this->insert('course_types', ['id' => 52, 'name' => 'Ballet']);
        $this->insert('terms', ['id' => 53, 'name' => 'Autumn', 'starts_on' => '2026-11-01', 'ends_on' => '2026-11-30']);
        foreach ([54 => '15:00:59', 55 => '15:00:00'] as $id => $startTime) {
            $this->insert('classes', [
                'id' => $id, 'term_id' => 53, 'course_type_id' => 52, 'room_id' => 51, 'instructor_id' => 21,
                'weekday' => 4, 'start_time' => $startTime, 'duration' => $this->postgres ? '1 hour' : 3600000000, 'capacity' => 8,
            ]);
            $this->insert('class_members', ['class_id' => $id, 'person_id' => 1, 'since' => '2026-11-01']);
        }
        $this->insert('lessons', [
            'id' => 56, 'course_type_id' => 52, 'room_id' => 51, 'instructor_id' => 21,
            'starts_at' => '2026-11-18 15:00:00.123456', 'ends_at' => '2026-11-18 16:00:00.123456',
            'kind' => 'regular', 'visibility' => 'public', 'status' => 'scheduled', 'capacity' => 8,
        ]);
        $this->insert('bookings', [
            'id' => 57, 'lesson_id' => 56, 'person_id' => 1, 'status' => 'canceled', 'booked_at' => $earlier,
            'canceled_at' => $stamp, 'late' => true, 'trial' => true, 'trial_note' => 'Trial note',
        ]);
        $this->insert('waitlist_entries', [
            'id' => 58, 'lesson_id' => 56, 'person_id' => 1, 'status' => 'withdrawn', 'stay_on_list' => false,
            'joined_at' => $earlier, 'invite_token_hash' => str_repeat('w', 32),
            'invited_at' => '2026-11-18 12:00:00', 'expires_at' => '2026-11-18 15:00:00',
        ]);
        $this->insert('pass_types', [
            'id' => 59, 'name' => 'Two visits', 'kind' => 'visits', 'visits' => 2,
            'validity_days' => 0, 'price' => 2000, 'vat_rate_bp' => 0, 'active' => true, 'created_at' => $earlier,
        ]);
        $this->insert('passes', [
            'id' => 60, 'pass_type_id' => 59, 'person_id' => 1, 'kind' => 'visits', 'visits_total' => 2,
            'visits_remaining' => 1, 'valid_from' => '2026-11-01', 'status' => 'active', 'origin' => 'assignment',
            'sold_at' => '2026-11-01 12:00:00', 'price_paid' => 0,
        ]);
        $this->insert('pass_redemptions', ['pass_id' => 60, 'booking_id' => 57, 'at' => $earlier]);
        $this->insert('events', [
            'id' => 62, 'slug' => 'fixture-workshop', 'title' => 'Fixture workshop', 'course_type_id' => 52,
            'capacity' => 7, 'status' => 'published', 'price' => 2500, 'vat_rate_bp' => 0,
            'created_at' => $earlier, 'updated_at' => $earlier,
        ]);
        foreach ([71 => [2500, null], 72 => [-2500, 71]] as $id => [$gross, $cancels]) {
            $this->insert('receipts', [
                'id' => $id, 'number' => "R-2026-$id", 'year' => 2026, 'sequence' => $id,
                'kind' => $cancels === null ? 'sale' : 'cancellation', 'cancels_receipt_id' => $cancels,
                'issued_at' => $stamp, 'issue_date' => '2026-11-19', 'locale' => 'en',
                'issuer_name' => 'Studio', 'issuer_address' => 'Road', 'issuer_tax_id' => '123',
                'tax_mode' => 'exempt', 'recipient_name' => 'Fixture recipient', 'payment_method' => 'cash',
                'currency' => 'EUR', 'net' => $gross, 'vat' => 0, 'gross' => $gross, 'payer_person_id' => 11,
            ]);
        }
        $this->insert('receipt_pdfs', ['receipt_id' => 71, 'pdf' => '%PDF-fixture']);
        $this->insert('event_registrations', [
            'id' => 61, 'event_id' => 62, 'person_id' => 1, 'status' => 'canceled', 'payment' => 'refunded',
            'price' => 2500, 'vat_rate_bp' => 0, 'receipt_id' => 71, 'cancellation_receipt_id' => 72,
            'created_at' => $earlier, 'updated_at' => $stamp, 'canceled_at' => $stamp,
        ]);
        $this->insert('calendar_feeds', [
            'id' => 63, 'account_id' => 1, 'scope' => 'schedule', 'location_id' => 50,
            'token_hash' => str_repeat('a', 32), 'created_at' => $stamp,
        ]);
        $this->insert('calendar_feeds', [
            'id' => 64, 'account_id' => 1, 'scope' => 'bookings',
            'token_hash' => str_repeat('b', 32), 'created_at' => $earlier, 'revoked_at' => $stamp,
        ]);
        $this->insert('calendar_feeds', ['id' => 68, 'account_id' => 2, 'scope' => 'bookings', 'token_hash' => str_repeat('c', 32), 'created_at' => $stamp]);
        $this->insert('push_subscriptions', [
            'id' => 65, 'account_id' => 1, 'endpoint' => 'https://push.example.test/secret', 'device_label' => 'Fixture Browser',
            'p256dh' => str_repeat('p', 65), 'auth' => str_repeat('a', 16), 'created_at' => $stamp, 'last_used_at' => $stamp,
        ]);
        $this->insert('push_subscriptions', [
            'id' => 69, 'account_id' => 2, 'endpoint' => 'https://push.example.test/teen',
            'p256dh' => str_repeat('q', 65), 'auth' => str_repeat('b', 16), 'created_at' => $stamp, 'last_used_at' => $stamp,
        ]);
        $this->insert('announcements', [
            'id' => 66, 'author_id' => 6, 'title' => 'Fixture announcement', 'body' => 'Other public text', 'audience' => 'everyone',
            'publish_at' => $stamp, 'email' => false, 'email_recipients' => 0, 'created_at' => $stamp, 'updated_at' => $stamp,
        ]);
        $this->insert('announcement_dismissals', ['announcement_id' => 66, 'account_id' => 1, 'dismissed_at' => $stamp]);
        $this->insert('announcement_dismissals', ['announcement_id' => 66, 'account_id' => 2, 'dismissed_at' => $stamp]);
        $this->insert('account_credentials', ['account_id' => 1, 'password_hash' => 'fixture-private-hash', 'changed_at' => $stamp]);
    }

    /** @param array<string,int|bool|string|null> $data */
    private function insert(string $table, array $data): void
    {
        $types = [];
        foreach ($data as $key => $value) {
            $types[$key] = match (true) {
                $value === null => ParameterType::NULL,
                is_bool($value) => ParameterType::BOOLEAN,
                is_int($value) => ParameterType::INTEGER,
                default => ParameterType::STRING,
            };
            if (is_int($value) && ($key === 'id' || str_ends_with($key, '_id') || $key === 'merged_into')) {
                $data[$key] = $this->uuid($value);
                $types[$key] = $this->postgres ? ParameterType::STRING : ParameterType::BINARY;
            }
        }
        $this->db->insert($table, $data, $types);
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
     * @phpstan-impure
     */
    private function request(string $method, string $path, array|string|null $input = null, ?string $user = 'parent', int $status = 200, string $mount = '/family'): array
    {
        $response = $this->response($method, $path, $input, $user, $mount);
        self::assertSame($status, $response->getStatusCode(), (string) $response->getBody());
        self::assertSame('no-store', $response->getHeaderLine('Cache-Control'));
        $body = json_decode((string) $response->getBody(), true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($body);

        return $body;
    }

    /** @param array<string,mixed>|string|null $input */
    private function response(string $method, string $path, array|string|null $input, ?string $user, string $mount): ResponseInterface
    {
        $request = (new ServerRequestFactory())->createServerRequest($method, self::BASE . '/api/v1' . $mount . $path);
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
        return Bootstrap::handle(
            static fn() => $request,
            dirname(__DIR__),
            '/missing',
            $this->postgresMigrations,
            static fn(string $key): ?string => $env[$key] ?? null,
            clock: $clock,
        );
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
