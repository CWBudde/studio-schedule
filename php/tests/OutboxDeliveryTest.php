<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use StudioSchedule\Mail\DeliveryFailure;
use StudioSchedule\Mail\Mailer;
use StudioSchedule\Service\OutboxDelivery;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\OutboxStore;
use StudioSchedule\Store\Schema;

final class OutboxDeliveryTest extends TestCase
{
    private const string NOW = '2026-11-18T12:00:00+00:00';
    private const string CREATED = '2026-11-18 10:00:00';
    private const string RECEIPT = '00000000-0000-7000-8000-000000000099';

    private Connection $db;
    private bool $postgres;
    private OutboxTestClock $clock;

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
        $this->clock = new OutboxTestClock(new DateTimeImmutable(self::NOW));
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

    public function testOneClaimPerSendInDueOrderAndAtMostTwentyPerRun(): void
    {
        for ($id = 21; $id >= 1; --$id) {
            $this->queue($id, due: $id === 12 ? '2026-11-18 11:00:00' : '2026-11-18 12:00:00');
        }
        $this->queue(23, due: '2026-11-18 12:00:01');
        $this->queue(24, due: null, text: null, sent: '2026-11-18 11:00:00');
        $store = new OutboxStore($this->db);
        $mailer = new OutboxTestMailer(function (string $to): void {
            self::assertFalse($this->db->isTransactionActive(), 'The delivery holds no database transaction');
            if ($to === 'fixture-12@example.test') {
                self::assertContains($this->db->fetchOne('SELECT attempts FROM outbox_mails WHERE id = ?', [$this->id(1)]), [0, '0']);
                self::assertContains($this->db->fetchOne('SELECT attempts FROM outbox_mails WHERE id = ?', [$this->id(12)]), [1, '1']);
            }
        });
        $delivery = new OutboxDelivery($store, $this->clock, $mailer);
        self::assertSame(0, (new OutboxDelivery($store, $this->clock, null))->deliver());
        self::assertSame(20, $delivery->deliver());
        $order = [12, ...array_diff(range(1, 20), [12])];
        self::assertSame(array_map(static fn(int $id): string => "fixture-$id@example.test", $order), array_column($mailer->sent, 0));
        self::assertSame(1, $delivery->deliver());
        self::assertSame(0, $delivery->deliver());
        self::assertSame('fixture-21@example.test', $mailer->sent[20][0]);
        self::assertNull($this->db->fetchOne('SELECT sent_at FROM outbox_mails WHERE id = ?', [$this->id(23)]));
        self::assertNull($this->db->fetchOne('SELECT body_text FROM outbox_mails WHERE id = ?', [$this->id(24)]));
    }

    public function testRetryScheduleAndSanitizedPermanentRejection(): void
    {
        $this->queue(1);
        $this->queue(2);
        $mailer = new OutboxTestMailer(static function (string $to): void {
            throw new DeliveryFailure('Private SMTP reply with a fixture secret', permanent: $to === 'fixture-2@example.test');
        });
        $delivery = new OutboxDelivery(new OutboxStore($this->db), $this->clock, $mailer);
        foreach (['+5 minutes', '+15 minutes', '+1 hour', '+3 hours', null] as $index => $delay) {
            self::assertSame(0, $delivery->deliver());
            $row = $this->state(1);
            self::assertSame($index + 1, $row['attempts']);
            self::assertNotNull($row['lastError']);
            self::assertStringNotContainsString('fixture secret', $row['lastError']);
            self::assertNull($row['sentAt']);
            if ($delay === null) {
                self::assertNull($row['nextAttemptAt']);
                continue;
            }
            $next = $this->clock->now()->modify($delay);
            self::assertSame($next->getTimestamp(), $row['nextAttemptAt']);
            self::assertSame(0, $delivery->deliver());
            self::assertSame($index + 1, $this->state(1)['attempts']);
            $this->clock->instant = $next;
        }
        self::assertSame(1, $this->state(2)['attempts']);
        self::assertNull($this->state(2)['nextAttemptAt']);
        self::assertNotNull($this->state(2)['lastError']);
        self::assertSame([], $mailer->sent);
    }

    public function testReceiptRequiredAndBlankBodyNeverSent(): void
    {
        $this->db->executeStatement(
            'INSERT INTO receipts (id, number, year, sequence, kind, issued_at, issue_date, locale, '
            . 'issuer_name, issuer_address, issuer_tax_id, tax_mode, recipient_name, payment_method, currency, net, vat, gross) '
            . "VALUES (?, 'TS-2026-1', 2026, 1, 'sale', '2026-11-18 09:00:00', '2026-11-18', 'en', "
            . "'School', 'Example address', 'Example tax ID', 'standard', 'Recipient', 'cash', 'EUR', 100, 19, 119)",
            [$this->storedId(self::RECEIPT)],
        );
        $this->queue(1, receipt: self::RECEIPT, replyTo: 'reply@example.test');
        $this->queue(2, text: "\t");
        $mailer = new OutboxTestMailer();
        $store = new OutboxStore($this->db);
        $read = static function (string $id): ?array {
            self::assertSame(self::RECEIPT, $id);

            throw new \RuntimeException('private receipt storage path');
        };
        self::assertSame(0, (new OutboxDelivery($store, $this->clock, $mailer, $read))->deliver());
        $missing = $this->state(1);
        self::assertSame(1, $missing['attempts']);
        self::assertNotNull($missing['lastError']);
        self::assertStringNotContainsString('private receipt', $missing['lastError']);
        self::assertNotNull($missing['nextAttemptAt']);
        self::assertNull($this->state(2)['nextAttemptAt']);
        self::assertSame([], $mailer->sent);

        $this->clock->instant = $this->clock->now()->modify('+5 minutes');
        $read = static function (string $id): array {
            self::assertSame(self::RECEIPT, $id);

            return ['filename' => 'receipt.pdf', 'pdf' => '%PDF-fixture'];
        };
        self::assertSame(1, (new OutboxDelivery($store, $this->clock, $mailer, $read))->deliver());
        self::assertSame([['fixture-1@example.test', 'Example subject', 'Example message', '', 'reply@example.test', 'receipt.pdf', '%PDF-fixture']], $mailer->sent);
        self::assertSame([
            'attempts' => 2, 'nextAttemptAt' => null, 'sentAt' => $this->clock->now()->getTimestamp(), 'lastError' => null,
        ], $this->state(1));
    }

    public function testStaleClaimCannotOverwriteItsSuccessorOrSendASixthTime(): void
    {
        $this->queue(1);
        $this->queue(2, attempts: 5);
        $store = new OutboxStore($this->db);
        $first = $store->claim($this->clock->now());
        self::assertNotNull($first);
        $this->clock->instant = $this->clock->now()->modify('+5 minutes');
        $second = $store->claim($this->clock->now());
        self::assertNotNull($second);
        self::assertSame($first['id'], $second['id']);
        self::assertFalse($store->record($first, $this->clock->now(), null, false));
        self::assertTrue($store->record($second, $this->clock->now(), null, false));
        self::assertSame(2, $this->state(1)['attempts']);

        $mailer = new OutboxTestMailer();
        self::assertSame(0, (new OutboxDelivery($store, $this->clock, $mailer))->deliver());
        self::assertSame(5, $this->state(2)['attempts']);
        self::assertNull($this->state(2)['nextAttemptAt']);
        self::assertNotNull($this->state(2)['lastError']);
        self::assertSame([], $mailer->sent);
    }

    private function queue(
        int $id,
        ?string $due = '2026-11-18 12:00:00',
        ?string $text = 'Example message',
        ?string $sent = null,
        ?string $receipt = null,
        ?string $replyTo = null,
        int $attempts = 0,
    ): void {
        $this->db->executeStatement(
            'INSERT INTO outbox_mails (id, recipient, subject, body_text, attempts, created_at, next_attempt_at, sent_at, receipt_id, reply_to) '
            . "VALUES (?, ?, 'Example subject', ?, ?, ?, ?, ?, ?, ?)",
            [$this->id($id), "fixture-$id@example.test", $text, $attempts, self::CREATED, $due, $sent,
                $receipt === null ? null : $this->storedId($receipt), $replyTo],
        );
    }

    /** @return array{attempts:int,nextAttemptAt:?int,sentAt:?int,lastError:?string} */
    private function state(int $id): array
    {
        $row = $this->db->fetchAssociative('SELECT attempts, next_attempt_at, sent_at, last_error FROM outbox_mails WHERE id = ?', [$this->id($id)]);
        self::assertIsArray($row);
        self::assertIsNumeric($row['attempts']);
        $lastError = $row['last_error'];
        self::assertTrue($lastError === null || is_string($lastError));

        return [
            'attempts' => (int) $row['attempts'],
            'nextAttemptAt' => $this->timestamp($row['next_attempt_at']),
            'sentAt' => $this->timestamp($row['sent_at']),
            'lastError' => $lastError,
        ];
    }

    private function timestamp(mixed $value): ?int
    {
        if ($value === null) {
            return null;
        }
        self::assertIsString($value);

        return (new DateTimeImmutable($value . ($this->postgres ? '' : ' UTC')))->getTimestamp();
    }

    private function id(int $value): string
    {
        return $this->storedId(sprintf('00000000-0000-7000-8000-%012x', $value));
    }

    private function storedId(string $uuid): string
    {
        return $this->postgres ? $uuid : pack('H*', str_replace('-', '', $uuid));
    }
}

final class OutboxTestClock implements ClockInterface
{
    public function __construct(public DateTimeImmutable $instant) {}

    public function now(): DateTimeImmutable
    {
        return $this->instant;
    }
}

final class OutboxTestMailer implements Mailer
{
    /** @var list<array{string,string,string,string,?string,?string,?string}> */
    public array $sent = [];

    /** @param ?Closure(string):void $onSend */
    public function __construct(private readonly ?Closure $onSend = null) {}

    public function send(
        string $to,
        string $subject,
        string $text,
        string $html,
        ?string $replyTo = null,
        ?string $attachmentName = null,
        ?string $attachmentBytes = null,
    ): void {
        if ($this->onSend !== null) {
            ($this->onSend)($to);
        }
        $this->sent[] = [$to, $subject, $text, $html, $replyTo, $attachmentName, $attachmentBytes];
    }
}
