<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Service\PassIssuePersistence;
use Symfony\Component\Uid\Uuid;

final readonly class PassIssueStore implements PassIssuePersistence
{
    public function __construct(
        private Connection $db,
        private FamilyStore $family,
        private PassReadStore $read,
        private EventStore $transactions,
    ) {}

    public function write(Closure $operation): mixed
    {
        return $this->transactions->write($operation);
    }

    public function livePerson(string $id): bool
    {
        return $this->family->hasLivePerson($id);
    }

    public function lockLivePersons(string ...$ids): bool
    {
        return $this->family->lockLivePersons(...$ids);
    }

    public function lockProduct(string $id): ?array
    {
        // Keep the sold terms stable while allowing concurrent sales and foreign key checks.
        $lock = $this->idType() === ParameterType::STRING ? 'FOR SHARE' : 'LOCK IN SHARE MODE';
        /** @var array{name:string,kind:string,visits:?numeric,validity_days:numeric,price:numeric,vat_rate_bp:numeric,active:bool|int|string}|false $row */
        $row = $this->db->fetchAssociative(
            "SELECT name, kind, visits, validity_days, price, vat_rate_bp, active FROM pass_types WHERE id = ? $lock",
            [$this->key($id)],
            [$this->idType()],
        );

        return $row === false ? null : [
            'name' => $row['name'], 'kind' => $row['kind'], 'visits' => $row['visits'] === null ? null : (int) $row['visits'],
            'days' => (int) $row['validity_days'], 'price' => (int) $row['price'], 'vatRate' => (int) $row['vat_rate_bp'],
            'active' => in_array($row['active'], [true, 1, '1', 't'], true),
        ];
    }

    public function create(array $pass): array
    {
        $types = [];
        $row = $pass;
        foreach (['id', 'person_id', 'pass_type_id', 'sold_by', 'payer_person_id', 'receipt_id'] as $field) {
            if ($pass[$field] !== null) {
                $row[$field] = $this->key($pass[$field]);
                $types[$field] = $this->idType();
            }
        }
        $row['sold_at'] = Database::sqlInstant($this->db, $pass['sold_at']);
        $this->db->insert('passes', $row, $types);

        return $this->read->byId($pass['id']) ?? throw new RuntimeException('The new pass disappeared');
    }

    private function idType(): ParameterType
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform ? ParameterType::STRING : ParameterType::BINARY;
    }

    private function key(string $id): string
    {
        return $this->idType() === ParameterType::STRING ? $id : Uuid::fromString($id)->toBinary();
    }
}
