<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\Psr7\Factory\StreamFactory;
use StudioSchedule\Bootstrap;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;

final class MailLogTest extends TestCase
{
    private const string BASE = 'https://school.example.test/dance';
    private const string CREATE = '2026-11-18 12:00:00.123456';
    private const string NOW = '2026-11-19T12:00:00.654321Z';

    private Connection $db;
    private bool $postgres;
    private string $dsn;
    private string $postgresMigrations;

    public function testMountedAdminMailLogFiltersPaginatesAndAuditsARequeue(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database from just php-test');
        }
        $this->dsn = $dsn;
        $this->db = Database::connect($dsn);
        $this->postgres = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $this->postgresMigrations = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations';
        self::assertSame([], (new Schema($this->db))->relations(), 'Requires an empty disposable database');
        try {
            (new Migrator($dsn))->up($this->postgres ? $this->postgresMigrations : dirname(__DIR__) . '/migrations/mysql');
            $this->account(1, 'admin');
            $this->account(2, 'office');
            $this->db->executeStatement(
                'INSERT INTO receipts (id, number, year, sequence, kind, issued_at, issue_date, locale, '
                . 'issuer_name, issuer_address, issuer_tax_id, tax_mode, recipient_name, payment_method, currency, net, vat, gross) '
                . "VALUES (?, 'TS-2026-1', 2026, 1, 'sale', '2026-11-18 09:00:00', '2026-11-18', 'en', "
                . "'School', 'Example address', 'Example tax ID', 'standard', 'Recipient', 'cash', 'EUR', 100, 19, 119)",
                [$this->id(50)],
            );
            $this->outbox(11, 'ÄLEX%name@example.test', [
                'template' => 'receipt', 'body_text' => 'Synthetic plain text.', 'body_html' => '<p>Synthetic HTML</p>',
                'attempts' => 5, 'last_error' => 'Temporary SMTP failure.', 'reply_to' => 'help@example.test', 'receipt_id' => $this->id(50),
            ]);
            $this->outbox(12, 'purged@example.test', ['sent_at' => '2026-11-18 13:00:00', 'body_text' => null]);
            $this->outbox(13, 'pending@example.test', ['next_attempt_at' => '2026-11-18 13:00:00']);
            foreach (['confirm-email', 'change-email', 'reset-password', 'invite', 'waitlist-invite', 'event-invite'] as $index => $template) {
                $this->outbox(20 + $index, 'links@example.test', ['template' => $template, 'body_text' => 'synthetic-private-link', 'body_html' => '<p>synthetic-private-link</p>']);
            }
            for ($i = 100; $i < 152; ++$i) {
                $this->outbox($i, 'batch@example.test');
            }

            self::assertSame(401, $this->request('POST', '/search', user: null, status: 401)['status']);
            self::assertSame('forbidden', $this->request('POST', '/search', user: 'office', input: '{', status: 403)['detail']);
            self::assertSame(403, $this->request('GET', '/' . self::uuid(11), user: 'office', status: 403)['status']);
            self::assertSame(403, $this->request('POST', '/' . self::uuid(11) . '/requeue', user: 'office', status: 403)['status']);
            $extraCursorComponent = rtrim(strtr(base64_encode('1795003200123456_' . self::uuid(102) . '_extra'), '+/', '-_'), '=');
            foreach ([['status' => 'queued'], ['template' => 'test'], ['cursor' => 'invalid'], ['cursor' => $extraCursorComponent],
                ['unexpected' => true], ['recipient' => str_repeat('a', 255)]] as $input) {
                self::assertSame(422, $this->request('POST', '/search', input: $input, status: 422)['status']);
            }
            foreach ([[null, 400], ['{', 400], ['[]', 422], [str_repeat(' ', 1024 * 1024), 413]] as [$input, $status]) {
                $this->request('POST', '/search', input: $input, status: $status);
            }

            $page = $this->request('POST', '/search', input: ['recipient' => 'BATCH@']);
            self::assertIsArray($page['items']);
            self::assertCount(50, $page['items']);
            self::assertSame(array_map(self::uuid(...), range(151, 102)), array_column($page['items'], 'id'));
            $cursor = $page['nextCursor'];
            self::assertIsString($cursor);
            self::assertSame('1795003200123456_' . self::uuid(102), base64_decode(strtr($cursor, '-_', '+/')));
            $rest = $this->request('POST', '/search', input: ['recipient' => 'batch@', 'cursor' => $cursor]);
            self::assertArrayNotHasKey('nextCursor', $rest);
            self::assertIsArray($rest['items']);
            self::assertSame([self::uuid(101), self::uuid(100)], array_column($rest['items'], 'id'));
            $literal = $this->request('POST', '/search', input: ['Status' => 'failed', 'template' => 'receipt', 'recipient' => 'Älex%NaMe']);
            self::assertIsArray($literal['items']);
            self::assertSame([self::uuid(11)], array_column($literal['items'], 'id'));
            self::assertIsArray($literal['items'][0]);
            self::assertArrayNotHasKey('text', $literal['items'][0]);
            self::assertSame('TS-2026-1.pdf', $literal['items'][0]['attachment']);
            self::assertSame(['items' => []], $this->request('POST', '/search', input: ['recipient' => 'älex%name']));
            self::assertSame(['items' => []], $this->request('POST', '/search', input: ['recipient' => '%other']));
            foreach (['sent' => 12, 'pending' => 13] as $state => $id) {
                $filtered = $this->request('POST', '/search', input: ['status' => $state]);
                self::assertIsArray($filtered['items']);
                self::assertSame([self::uuid($id)], array_column($filtered['items'], 'id'));
            }

            $shown = $this->request('GET', '/' . self::uuid(11));
            self::assertSame('shown', $shown['body']);
            self::assertSame('Synthetic plain text.', $shown['text']);
            self::assertSame('<p>Synthetic HTML</p>', $shown['html']);
            self::assertSame('help@example.test', $shown['replyTo']);
            $purged = $this->request('GET', '/' . self::uuid(12));
            self::assertSame('purged', $purged['body']);
            self::assertArrayNotHasKey('text', $purged);
            for ($i = 20; $i < 26; ++$i) {
                $id = '/' . self::uuid($i);
                $hidden = $this->request('GET', $id);
                self::assertSame('withheld', $hidden['body']);
                self::assertArrayNotHasKey('text', $hidden);
                self::assertArrayNotHasKey('html', $hidden);
                self::assertSame('secret-link', $this->request('POST', $id . '/requeue', status: 409)['reason']);
            }
            foreach ([12, 13] as $number) {
                self::assertSame('not-failed', $this->request('POST', '/' . self::uuid($number) . '/requeue', status: 409)['reason']);
            }
            $this->request('GET', '/' . self::uuid(99), status: 404);
            $this->request('GET', '/invalid-id', status: 422);
            $this->request('POST', '/' . self::uuid(99) . '/requeue', status: 404);

            $requeue = '/' . self::uuid(11) . '/requeue';
            $this->db->executeStatement("DELETE FROM audit_actions WHERE action = 'mail-requeued'");
            $this->request('POST', $requeue, status: 500);
            self::assertSame('failed', $this->request('GET', '/' . self::uuid(11))['status']);
            $this->db->executeStatement("INSERT INTO audit_actions (action) VALUES ('mail-requeued')");
            $again = $this->request('POST', $requeue);
            self::assertSame('pending', $again['status']);
            self::assertSame(0, $again['attempts']);
            self::assertSame(self::NOW, $again['nextAttemptAt']);
            self::assertSame('Temporary SMTP failure.', $again['lastError']);
            self::assertSame('TS-2026-1.pdf', $again['attachment']);
            self::assertArrayNotHasKey('text', $again);
            self::assertSame('not-failed', $this->request('POST', $requeue, status: 409)['reason']);
            self::assertSame($this->id(1), $this->db->fetchOne("SELECT actor_id FROM audit_events WHERE action = 'mail-requeued'"));
            $details = $this->db->fetchOne("SELECT details FROM audit_events WHERE action = 'mail-requeued'");
            self::assertIsString($details);
            self::assertEquals(['mailId' => self::uuid(11), 'recipient' => 'ÄLEX%name@example.test', 'template' => 'receipt'], json_decode($details, true, flags: JSON_THROW_ON_ERROR));
            self::assertContains($this->db->fetchOne("SELECT COUNT(*) FROM audit_events WHERE action = 'mail-requeued'"), [1, '1']);
        } finally {
            if (!$this->postgres) {
                $this->db->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            foreach ($this->db->createSchemaManager()->listTableNames() as $table) {
                $this->db->executeStatement('DROP TABLE ' . $this->db->quoteSingleIdentifier($table) . ($this->postgres ? ' CASCADE' : ''));
            }
            if ($this->postgres) {
                $this->db->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
            $this->db->close();
        }
    }

    /**
     * @param array<string,mixed>|string|null $input
     * @return array<array-key,mixed>
     */
    private function request(string $method, string $path, ?string $user = 'admin', array|string|null $input = null, int $status = 200): array
    {
        $request = (new ServerRequestFactory())->createServerRequest($method, self::BASE . '/api/v1/admin/mails' . $path);
        if ($user !== null) {
            $request = $request->withCookieParams(['studio_session' => 'fixture-' . $user]);
        }
        if ($input !== null) {
            $request = $request->withHeader('Content-Type', 'application/json')
                ->withBody((new StreamFactory())->createStream(is_array($input) ? json_encode($input, JSON_THROW_ON_ERROR) : $input));
        }
        $env = ['DATABASE_URL' => $this->dsn, 'BASE_URL' => self::BASE, 'LOG_LEVEL' => 'error'];
        $clock = new class implements ClockInterface {
            public function now(): DateTimeImmutable
            {
                return new DateTimeImmutable('2026-11-19T12:00:00.654321Z');
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

    /** @param array<string,mixed> $fields */
    private function outbox(int $id, string $recipient, array $fields = []): void
    {
        $this->db->insert('outbox_mails', $fields + [
            'id' => $this->id($id), 'recipient' => $recipient, 'subject' => 'Fixture mail',
            'created_at' => self::CREATE, 'body_text' => 'Fixture mail body.', 'attempts' => 0,
        ]);
    }

    private function account(int $id, string $role): void
    {
        $this->db->executeStatement("INSERT INTO persons (id, given_name, family_name) VALUES (?, 'Fixture', 'Person')", [$this->id($id)]);
        $this->db->executeStatement(
            'INSERT INTO accounts (id, person_id, email, role, confirmed_at) VALUES (?, ?, ?, ?, ?)',
            [$this->id($id), $this->id($id), $role . '@example.test', $role, self::CREATE],
        );
        $hash = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->db->executeStatement(
            "INSERT INTO sessions (id, account_id, token_hash, created_at, last_seen_at, expires_at) VALUES (?, ?, $hash, ?, ?, ?)",
            [$this->id(10 + $id), $this->id($id), hash('sha256', 'fixture-' . $role), self::CREATE, '2026-11-19 11:59:00', '2026-11-19 13:00:00'],
        );
    }

    private function id(int $id): string
    {
        $uuid = self::uuid($id);

        return $this->postgres ? $uuid : pack('H*', str_replace('-', '', $uuid));
    }

    private static function uuid(int $id): string
    {
        return sprintf('018f0000-0000-7000-8000-%012x', $id);
    }
}
