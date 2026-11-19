<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\ForeignKeyConstraintViolationException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use StudioSchedule\Service\Impersonation;
use StudioSchedule\Service\ImpersonationFailure;
use StudioSchedule\Service\SessionAuthentication;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;
use StudioSchedule\Store\SessionStore;

final class SessionImpersonationTest extends TestCase
{
    private const ADMIN = '018f0000-0000-7000-8000-000000000001';
    private const MEMBER = '018f0000-0000-7000-7000-0000000000ab';
    private const OTHER = '018f0000-0000-7000-8000-000000000003';
    private const SESSION = '018f0000-0000-7000-8000-000000000004';
    private const NOW = '2026-12-04 12:00:00';
    private const TOKEN = 'fictional-admin-session';

    private Connection $db;
    private bool $postgres;
    private SessionStore $sessions;
    private ClockInterface $clock;

    protected function setUp(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database from just php-test');
        }
        $db = Database::connect($dsn);
        self::assertSame([], (new Schema($db))->relations(), 'Requires an empty disposable database');
        $this->db = $db;
        $this->postgres = $db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $pgPath = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations';
        (new Migrator($dsn))->up($this->postgres ? $pgPath : dirname(__DIR__) . '/migrations/mysql');

        $this->sessions = new SessionStore($db);
        $this->clock = new class (new DateTimeImmutable(self::NOW, new DateTimeZone('UTC'))) implements ClockInterface {
            public function __construct(private readonly DateTimeImmutable $instant) {}

            public function now(): DateTimeImmutable
            {
                return $this->instant;
            }
        };
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

    public function testStartAndStopRotateTheSessionAndAuditTheActualActor(): void
    {
        $admin = $this->startFixture();
        $auth = new SessionAuthentication($this->sessions, $this->clock);
        $service = new Impersonation($this->sessions, $this->clock);
        $started = $service->start($admin, self::TOKEN, strtoupper(self::MEMBER));
        self::assertIsArray($started);
        $member = $started['principal'];
        self::assertSame(self::MEMBER, $member->accountId);
        self::assertSame('member', $member->role);
        self::assertSame(self::ADMIN, $member->impersonatorId);
        self::assertNotSame(self::TOKEN, $started['token']);
        self::assertSame('2026-12-04 13:00:00', $started['expiresAt']->format('Y-m-d H:i:s'));
        self::assertNull($auth->authenticate(self::TOKEN));

        $stored = $auth->authenticate($started['token']);
        self::assertEquals(['principal' => $member, 'renewedUntil' => null], $stored);
        self::assertSame(ImpersonationFailure::NoSession, $service->start($admin, self::TOKEN, self::MEMBER));
        self::assertSame(ImpersonationFailure::NotImpersonable, $service->start($member, $started['token'], self::MEMBER));

        $stopped = $service->stop($member, $started['token']);
        self::assertIsArray($stopped);
        self::assertEquals($admin, $stopped['principal']);
        self::assertNotSame(self::TOKEN, $stopped['token']);
        self::assertNotSame($started['token'], $stopped['token']);
        self::assertSame('2026-12-18 12:00:00', $stopped['expiresAt']->format('Y-m-d H:i:s'));
        self::assertNull($auth->authenticate($started['token']));
        self::assertEquals(['principal' => $admin, 'renewedUntil' => null], $auth->authenticate($stopped['token']));
        self::assertSame(ImpersonationFailure::NoSession, $service->stop($member, $started['token']));
        self::assertSame(ImpersonationFailure::NotImpersonating, $service->stop($admin, $stopped['token']));

        self::assertSame([
            ['actor_id' => $this->id(self::ADMIN), 'action' => 'impersonation-started', 'target_kind' => 'account', 'target_id' => $this->id(self::MEMBER), 'details' => null],
            ['actor_id' => $this->id(self::ADMIN), 'action' => 'impersonation-ended', 'target_kind' => 'account', 'target_id' => $this->id(self::MEMBER), 'details' => null],
        ], $this->db->fetchAllAssociative('SELECT actor_id, action, target_kind, target_id, details FROM audit_events ORDER BY action DESC'));
    }

    public function testEligibilityAndSessionExpiryAreRecheckedBeforeRotation(): void
    {
        $admin = $this->startFixture();
        $service = new Impersonation($this->sessions, $this->clock);
        self::assertSame(ImpersonationFailure::NotImpersonable, $service->start($admin, self::TOKEN, self::ADMIN));
        self::assertSame(ImpersonationFailure::AccountNotFound, $service->start($admin, self::TOKEN, self::OTHER));
        $this->db->executeStatement('UPDATE accounts SET confirmed_at = NULL WHERE id = ?', [$this->id(self::MEMBER)]);
        self::assertSame(ImpersonationFailure::AccountInactive, $service->start($admin, self::TOKEN, self::MEMBER));
        $this->db->executeStatement("UPDATE accounts SET confirmed_at = ?, role = 'admin' WHERE id = ?", [self::NOW, $this->id(self::MEMBER)]);
        self::assertSame(ImpersonationFailure::NotImpersonable, $service->start($admin, self::TOKEN, self::MEMBER));
        $this->db->executeStatement("UPDATE accounts SET role = 'member' WHERE id = ?", [$this->id(self::MEMBER)]);
        $this->db->executeStatement("UPDATE accounts SET role = 'office' WHERE id = ?", [$this->id(self::ADMIN)]);
        self::assertSame(ImpersonationFailure::NoSession, $service->start($admin, self::TOKEN, self::MEMBER));
        $this->db->executeStatement("UPDATE accounts SET role = 'admin' WHERE id = ?", [$this->id(self::ADMIN)]);

        $started = $service->start($admin, self::TOKEN, self::MEMBER);
        self::assertIsArray($started);
        $this->db->executeStatement('UPDATE accounts SET disabled_at = ? WHERE id = ?', [self::NOW, $this->id(self::ADMIN)]);
        self::assertSame(ImpersonationFailure::NoSession, $service->stop($started['principal'], $started['token']));
        $this->db->executeStatement('UPDATE accounts SET disabled_at = NULL');
        $atExpiry = self::createStub(ClockInterface::class);
        $atExpiry->method('now')->willReturn($started['expiresAt']);
        $expired = new Impersonation($this->sessions, $atExpiry);
        self::assertSame(ImpersonationFailure::NoSession, $expired->stop($started['principal'], $started['token']));
        self::assertNull((new SessionAuthentication($this->sessions, $atExpiry))->authenticate($started['token']));
        self::assertSame(['impersonation-started'], $this->db->fetchFirstColumn('SELECT action FROM audit_events'));
    }

    public function testFailedAuditRollsBackSessionRotation(): void
    {
        $admin = $this->startFixture();
        $this->db->executeStatement("DELETE FROM audit_actions WHERE action = 'impersonation-started'");
        $service = new Impersonation($this->sessions, $this->clock);

        try {
            $service->start($admin, self::TOKEN, self::MEMBER);
            self::fail('The missing action must fail the audit insert');
        } catch (ForeignKeyConstraintViolationException) {
            $auth = new SessionAuthentication($this->sessions, $this->clock);
            self::assertEquals(['principal' => $admin, 'renewedUntil' => null], $auth->authenticate(self::TOKEN));
            self::assertSame([$this->id(self::ADMIN)], $this->db->fetchFirstColumn('SELECT account_id FROM sessions'));
            self::assertSame([], $this->db->fetchFirstColumn('SELECT action FROM audit_events'));
        }
    }

    private function startFixture(): SessionPrincipal
    {
        foreach ([self::ADMIN => 'admin', self::MEMBER => 'member'] as $account => $role) {
            $id = $this->id($account);
            $this->db->executeStatement("INSERT INTO persons (id, given_name, family_name) VALUES (?, 'Ari', 'Fable')", [$id]);
            $this->db->executeStatement(
                'INSERT INTO accounts (id, person_id, email, role, confirmed_at) VALUES (?, ?, ?, ?, ?)',
                [$id, $id, $role . '@example.test', $role, self::NOW],
            );
        }
        $digest = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->db->executeStatement(
            "INSERT INTO sessions (id, account_id, token_hash, created_at, last_seen_at, expires_at) VALUES (?, ?, $digest, ?, ?, ?)",
            [$this->id(self::SESSION), $this->id(self::ADMIN), hash('sha256', self::TOKEN), self::NOW, self::NOW, '2026-12-05 12:00:00'],
        );

        return new SessionPrincipal(self::ADMIN, self::ADMIN, 'admin');
    }

    private function id(string $uuid): string
    {
        return $this->postgres ? $uuid : pack('H*', str_replace('-', '', $uuid));
    }
}
