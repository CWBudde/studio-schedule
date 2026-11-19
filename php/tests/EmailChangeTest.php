<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Closure;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\DriverException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use StudioSchedule\Mail\Renderer;
use StudioSchedule\Service\EmailChange;
use StudioSchedule\Service\EmailChangeFailure;
use StudioSchedule\Service\PasswordHash;
use StudioSchedule\Service\SchoolSetup;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SetupInput;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\EmailChangeStore;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;
use StudioSchedule\Store\SetupStore;
use Symfony\Component\Uid\Uuid;

final class EmailChangeTest extends TestCase
{
    private const string OLD = 'Current@example.test';
    private const string NEW = 'New@example.test';
    private const string NOW = '2026-12-04 12:00:00';
    private const string SESSION = 'fictional account session';
    private const string PASSWORD = 'fictional current passphrase';

    private Connection $db;
    private bool $postgres;
    private PasswordHash $passwords;
    private ClockInterface $clock;
    private SessionPrincipal $principal;

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

        $this->passwords = new PasswordHash();
        $this->clock = $this->clockAt(self::NOW);
        $account = (new SchoolSetup(new SetupStore($db), $this->clock))->init(
            new SetupInput('Test Studio', 'Europe/Berlin', 'de', 'EUR', self::OLD, 'Ada', 'Example'),
        );
        $accountId = $this->id($account->accountId);
        $db->executeStatement('UPDATE accounts SET confirmed_at = ? WHERE id = ?', [self::NOW, $accountId]);
        $db->insert('account_credentials', [
            'account_id' => $accountId, 'password_hash' => $this->passwords->hash(self::PASSWORD), 'changed_at' => self::NOW,
        ]);
        $db->executeStatement(
            'INSERT INTO sessions (id, account_id, token_hash, created_at, last_seen_at, expires_at) '
            . 'VALUES (?, ?, ' . $this->digestSql() . ', ?, ?, ?)',
            [$this->id(Uuid::v7()->toRfc4122()), $accountId, hash('sha256', self::SESSION), self::NOW, self::NOW, '2026-12-06 12:00:00'],
        );
        $personId = $db->fetchOne('SELECT person_id FROM accounts');
        self::assertIsString($personId);
        $this->principal = new SessionPrincipal($account->accountId, $this->postgres ? $personId : Uuid::fromBinary($personId)->toRfc4122(), 'admin');
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

    public function testRequestReplacesTheLinkAndConfirmationSwitchesEmailNotifiesOldAddressAndKeepsSession(): void
    {
        $wakes = 0;
        $wakeMail = function () use (&$wakes): void {
            self::assertFalse($this->db->isTransactionActive());
            ++$wakes;
        };
        $change = $this->service(wakeMail: $wakeMail);
        $actor = $this->principal;
        self::assertSame(EmailChangeFailure::WrongPassword, $change->request($actor, self::SESSION, 'incorrect', self::NEW, ''));
        self::assertSame(EmailChangeFailure::SameEmail, $change->request($actor, self::SESSION, 'incorrect', 'current@example.test', ''));
        self::assertSame(EmailChangeFailure::NoSession, $change->request($actor, 'wrong-session', self::PASSWORD, self::NEW, ''));
        self::assertSame(EmailChangeFailure::NoSession, $this->service($this->clockAt('2026-12-06 12:00:00'))
            ->request($actor, self::SESSION, self::PASSWORD, self::NEW, ''));
        $impersonated = new SessionPrincipal($actor->accountId, $actor->personId, 'admin', impersonatorId: Uuid::v7()->toRfc4122());
        self::assertSame(EmailChangeFailure::Impersonating, $change->request($impersonated, self::SESSION, self::PASSWORD, self::NEW, ''));
        self::assertSame(0, $wakes);

        $target = 'Replacement@example.test';
        self::assertNull($change->request($actor, self::SESSION, self::PASSWORD, "\u{2003}" . self::NEW . ' ', 'en'));
        $oldLink = $this->link(self::NEW);
        self::assertNull($change->request($actor, self::SESSION, self::PASSWORD, $target, 'en'));
        self::assertSame(2, $wakes);
        $link = $this->link($target);
        self::assertSame(self::OLD, $this->email());
        self::assertSame(EmailChangeFailure::InvalidToken, $change->confirm($oldLink, ''));
        self::assertSame(EmailChangeFailure::InvalidToken, $this->service($this->clockAt('2026-12-05 12:00:00'))->confirm($link, ''));

        $digest = $this->db->fetchOne('SELECT ' . ($this->postgres ? "encode(token_hash, 'hex')" : 'LOWER(HEX(token_hash))')
            . " FROM account_tokens WHERE purpose = 'change-email'");
        self::assertSame(hash('sha256', $link), $digest);
        self::assertNull($this->service($this->clockAt('2026-12-05 11:59:59.999999'), $wakeMail)->confirm($link, 'en'));
        self::assertSame(3, $wakes);
        self::assertSame($target, $this->email());
        self::assertSame(EmailChangeFailure::InvalidToken, $change->confirm($link, ''));
        self::assertSame([], $this->db->fetchFirstColumn('SELECT token_hash FROM account_tokens'));
        self::assertCount(1, $this->db->fetchFirstColumn('SELECT id FROM sessions'));

        $notice = $this->db->fetchAssociative("SELECT recipient, subject, body_text FROM outbox_mails WHERE template = 'email-changed'");
        self::assertIsArray($notice);
        self::assertSame(self::OLD, $notice['recipient']);
        self::assertSame('Deine E-Mail-Adresse bei Test Studio wurde geändert', $notice['subject']);
        self::assertIsString($notice['body_text']);
        self::assertStringContainsString($target, $notice['body_text']);
    }

    public function testTakenAddressesAndUnsendableNoticesPreserveTheCurrentAddress(): void
    {
        $change = $this->service();
        self::assertNull($change->request($this->principal, self::SESSION, self::PASSWORD, self::NEW, ''));
        $link = $this->link(self::NEW);

        $other = $this->id(Uuid::v7()->toRfc4122());
        $this->db->insert('persons', ['id' => $other, 'given_name' => 'Other', 'family_name' => 'Fixture']);
        $this->db->insert('accounts', ['id' => $other, 'person_id' => $other, 'email' => strtolower(self::NEW), 'role' => 'member']);
        self::assertNull($change->request($this->principal, self::SESSION, self::PASSWORD, self::NEW, 'en'));
        self::assertCount(1, $this->db->fetchFirstColumn('SELECT id FROM outbox_mails'));
        self::assertSame(EmailChangeFailure::EmailTaken, $change->confirm($link, ''));
        self::assertSame(self::OLD, $this->email());

        $this->db->executeStatement('DELETE FROM accounts WHERE id = ?', [$other]);
        $this->db->executeStatement("ALTER TABLE outbox_mails ADD CONSTRAINT fixture_no_notice CHECK (template <> 'email-changed')");
        try {
            $change->confirm($link, '');
            self::fail('The outbox should reject the notice in this fixture');
        } catch (DriverException) {
            self::assertSame(self::OLD, $this->email());
            self::assertCount(2, $this->db->fetchFirstColumn('SELECT token_hash FROM account_tokens'));
            self::assertCount(1, $this->db->fetchFirstColumn('SELECT id FROM outbox_mails'));
            self::assertCount(1, $this->db->fetchFirstColumn('SELECT id FROM sessions'));
        }
    }

    /** @param (Closure():void)|null $wakeMail */
    private function service(?ClockInterface $clock = null, ?Closure $wakeMail = null): EmailChange
    {
        return new EmailChange(new EmailChangeStore($this->db), $this->passwords, $clock ?? $this->clock, new Renderer(), 'https://example.test/studio', $wakeMail);
    }

    private function clockAt(string $value): ClockInterface
    {
        $clock = self::createStub(ClockInterface::class);
        $clock->method('now')->willReturn(new DateTimeImmutable($value, new DateTimeZone('UTC')));

        return $clock;
    }

    /** @phpstan-impure */
    private function email(): string
    {
        $email = $this->db->fetchOne('SELECT email FROM accounts WHERE id = ?', [$this->id($this->principal->accountId)]);
        self::assertIsString($email);

        return $email;
    }

    private function link(string $recipient): string
    {
        $body = $this->db->fetchOne("SELECT body_text FROM outbox_mails WHERE template = 'change-email' AND recipient = ?", [$recipient]);
        self::assertIsString($body);
        if (preg_match('~https://example\.test/studio/confirm-email-change\?token=([A-Za-z0-9_-]{43})~', $body, $matches) !== 1) {
            self::fail('The outbox mail should carry a mounted confirmation link');
        }

        return $matches[1];
    }

    private function digestSql(): string
    {
        return $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
    }

    private function id(string $uuid): string
    {
        return $this->postgres ? $uuid : Uuid::fromString($uuid)->toBinary();
    }
}
