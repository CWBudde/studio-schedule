<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use Symfony\Component\Uid\Uuid;

final readonly class AdminAccountsStore
{
    public function __construct(private Connection $connection) {}

    /** @return list<array{id:string,email:string,role:string,givenName:string,familyName:string,confirmed:bool,disabled:bool}> */
    public function list(): array
    {
        $postgres = $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
        // The account email indexes fold ASCII case and preserve all other bytes.
        $emailOrder = $postgres ? 'lower(a.email)' : 'a.email_folded';
        $rows = $this->connection->fetchAllAssociative(
            'SELECT a.id, a.email, a.role, p.given_name, p.family_name, a.confirmed_at, a.disabled_at '
            . "FROM accounts a JOIN persons p ON p.id = a.person_id ORDER BY $emailOrder, a.id",
        );

        $items = [];
        foreach ($rows as $row) {
            assert(is_string($row['id']) && is_string($row['email']) && is_string($row['role'])
                && is_string($row['given_name']) && is_string($row['family_name']));
            $items[] = [
                'id' => $postgres ? $row['id'] : Uuid::fromBinary($row['id'])->toRfc4122(),
                'email' => $row['email'],
                'role' => $row['role'],
                'givenName' => $row['given_name'],
                'familyName' => $row['family_name'],
                'confirmed' => $row['confirmed_at'] !== null,
                'disabled' => $row['disabled_at'] !== null,
            ];
        }

        return $items;
    }
}
