<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Http\Message\ResponseInterface;
use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\Psr7\Response;
use StudioSchedule\Http\AdminAccountsEndpoint;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Store\AdminAccountsStore;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;
use Symfony\Component\Uid\Uuid;

final class AdminAccountsEndpointTest extends TestCase
{
    public function testAuthorizationRejectsOtherPrincipalsBeforeQueryingTheList(): void
    {
        $db = self::createMock(Connection::class);
        $db->expects(self::never())->method('getDatabasePlatform');
        $db->expects(self::never())->method('fetchAllAssociative');
        $list = new AdminAccountsEndpoint(new AdminAccountsStore($db));

        $guest = self::request($list);
        self::assertSame(401, $guest->getStatusCode());
        self::assertSame('application/problem+json', $guest->getHeaderLine('Content-Type'));
        self::assertSame('not signed in', self::body($guest)['detail']);

        foreach (['member', 'instructor', 'office', 'admin'] as $role) {
            $principal = new SessionPrincipal(self::id(1), self::id(2), $role, impersonatorId: $role === 'admin' ? self::id(3) : null);
            $rejected = self::request($list, $principal);
            self::assertSame(403, $rejected->getStatusCode());
            self::assertSame('application/problem+json', $rejected->getHeaderLine('Content-Type'));
            self::assertSame('forbidden', self::body($rejected)['detail']);
        }
    }

    public function testAdminGetsEveryAccountInAsciiEmailOrderWithOnlyPickerFields(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database from just php-test');
        }
        $db = Database::connect($dsn);
        self::assertSame([], (new Schema($db))->relations(), 'Account listing tests require an empty disposable database');
        $postgres = $db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $migrations = $postgres
            ? (getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations')
            : dirname(__DIR__) . '/migrations/mysql';

        try {
            (new Migrator($dsn))->up($migrations);
            $list = new AdminAccountsEndpoint(new AdminAccountsStore($db));
            $principal = new SessionPrincipal(self::id(1), self::id(10), 'admin');
            $empty = self::request($list, $principal);
            self::assertSame(200, $empty->getStatusCode());
            self::assertSame(['items' => []], self::body($empty));

            $fixtures = [
                ['email' => 'Zane@example.test', 'role' => 'admin', 'givenName' => 'Zane', 'confirmed' => true, 'disabled' => false],
                ['email' => 'änne@example.test', 'role' => 'member', 'givenName' => 'änne', 'confirmed' => false, 'disabled' => false],
                ['email' => 'amy@example.test', 'role' => 'office', 'givenName' => 'Amy', 'confirmed' => true, 'disabled' => false],
                ['email' => 'Änne@example.test', 'role' => 'instructor', 'givenName' => 'Änne', 'confirmed' => true, 'disabled' => false],
                ['email' => 'Axe@example.test', 'role' => 'member', 'givenName' => 'Axe', 'confirmed' => false, 'disabled' => true],
            ];
            foreach ($fixtures as $i => &$item) {
                $item = [
                    'id' => self::id($i + 1), 'email' => $item['email'], 'role' => $item['role'],
                    'givenName' => $item['givenName'], 'familyName' => 'Fixture',
                    'confirmed' => $item['confirmed'], 'disabled' => $item['disabled'],
                ];
                $accountId = self::databaseId($item['id'], $postgres);
                $personId = self::databaseId(self::id($i + 10), $postgres);
                $db->insert('persons', [
                    'id' => $personId,
                    'given_name' => $item['givenName'],
                    'family_name' => $item['familyName'],
                    'email' => 'private-person@example.test',
                    'birth_date' => '1990-01-01',
                    'merged_into' => $item['disabled'] ? self::databaseId(self::id(10), $postgres) : null,
                ]);
                $db->insert('accounts', [
                    'id' => $accountId, 'person_id' => $personId, 'email' => $item['email'], 'role' => $item['role'],
                    'confirmed_at' => $item['confirmed'] ? '2026-11-14 12:00:00.123456' : null,
                    'disabled_at' => $item['disabled'] ? '2026-11-15 12:00:00.123456' : null,
                    'visible_days' => 17, 'locale' => 'en',
                ]);
            }
            unset($item);

            $response = self::request($list, $principal);
            self::assertSame(200, $response->getStatusCode());
            self::assertSame('application/json', $response->getHeaderLine('Content-Type'));
            self::assertSame(['items' => [$fixtures[2], $fixtures[4], $fixtures[0], $fixtures[3], $fixtures[1]]], self::body($response));
        } finally {
            if (!$postgres) {
                $db->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            foreach ($db->createSchemaManager()->listTableNames() as $table) {
                $db->executeStatement('DROP TABLE IF EXISTS ' . $db->quoteSingleIdentifier($table) . ($postgres ? ' CASCADE' : ''));
            }
            if ($postgres) {
                $db->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
            $db->close();
        }
    }

    private static function request(AdminAccountsEndpoint $endpoint, ?SessionPrincipal $principal = null): ResponseInterface
    {
        $request = (new ServerRequestFactory())->createServerRequest('GET', '/api/v1/admin/accounts');

        return $endpoint($request->withAttribute('sessionPrincipal', $principal), new Response());
    }

    /** @return array<array-key, mixed> */
    private static function body(ResponseInterface $response): array
    {
        $body = json_decode((string) $response->getBody(), true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($body);

        return $body;
    }

    private static function id(int $index): string
    {
        return sprintf('018f0000-0000-7000-8000-%012x', $index);
    }

    private static function databaseId(string $id, bool $postgres): string
    {
        return $postgres ? $id : Uuid::fromString($id)->toBinary();
    }
}
