<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Service\PassReadPersistence;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type PassDocument from PassReadPersistence
 * @phpstan-import-type PersonRef from PassReadPersistence
 */
final readonly class PassReadStore implements PassReadPersistence
{
    private const int MAX_USES_PER_PASS = 10;

    public function __construct(private Connection $connection, private ScheduleStore $schedule) {}

    public function read(Closure $read): mixed
    {
        return Database::inReadSnapshot($this->connection, $read);
    }

    public function school(): ?array
    {
        /** @var array{timezone:string,redemption_mode:string}|false $row */
        $row = $this->connection->fetchAssociative('SELECT timezone, redemption_mode FROM settings');

        return $row === false ? null : $row;
    }

    public function personExists(string $id): bool
    {
        return $this->connection->fetchOne('SELECT 1 FROM persons WHERE id = ?', [$this->key($id)], [$this->keyType()]) !== false;
    }

    public function passes(string $person, bool $family): array
    {
        $scope = $this->ownerScope($family);
        $note = $family ? 'NULL AS note' : 'p.note';
        $rows = $this->connection->fetchAllAssociative(
            'SELECT p.id, p.pass_type_id, t.name AS type_name, p.person_id, p.payer_person_id, p.kind, '
            . 'COALESCE(p.visits_total, 0) AS visits_total, COALESCE(p.visits_remaining, 0) AS visits_remaining, '
            . 'p.valid_from, p.valid_until, p.status, p.origin, p.sold_at, p.price_paid, p.voided_at, p.receipt_id, '
            . "CONCAT(owner.given_name, ' ', owner.family_name) AS owner_name, owner.anonymized_at AS owner_anonymized_at, "
            . "CONCAT(payer.given_name, ' ', payer.family_name) AS payer_name, payer.anonymized_at AS payer_anonymized_at, $note "
            . 'FROM passes p JOIN pass_types t ON t.id = p.pass_type_id JOIN persons owner ON owner.id = p.person_id '
            . "LEFT JOIN persons payer ON payer.id = p.payer_person_id WHERE $scope ORDER BY p.sold_at DESC, p.id DESC",
            $this->ownerKeys($person, $family),
            array_fill(0, $family ? 2 : 1, $this->keyType()),
        );

        return array_map($this->pass(...), $rows);
    }

    public function redemptions(string $person, bool $family): array
    {
        $scope = $this->ownerScope($family);
        $rows = $this->connection->fetchAllAssociative(
            'SELECT recent.pass_id, recent.booking_id, recent.at, recent.use_count, b.lesson_id, b.status, b.late FROM ('
            . 'SELECT r.pass_id, r.booking_id, r.at, COUNT(*) OVER (PARTITION BY r.pass_id) AS use_count, '
            . 'ROW_NUMBER() OVER (PARTITION BY r.pass_id ORDER BY r.at DESC, r.booking_id DESC) AS ranking '
            . "FROM pass_redemptions r JOIN passes p ON p.id = r.pass_id WHERE $scope"
            . ') recent JOIN bookings b ON b.id = recent.booking_id '
            . 'WHERE recent.ranking <= ' . self::MAX_USES_PER_PASS . ' ORDER BY recent.pass_id, recent.at, recent.booking_id',
            $this->ownerKeys($person, $family),
            array_fill(0, $family ? 2 : 1, $this->keyType()),
        );

        return array_map(fn(array $row): array => [
            'passId' => $this->id($row['pass_id']), 'bookingId' => $this->id($row['booking_id']),
            'lessonId' => $this->id($row['lesson_id']), 'bookingStatus' => self::text($row['status']),
            'late' => in_array($row['late'], [true, 1, '1', 't'], true),
            'redeemedAt' => self::instant($row['at']), 'useCount' => self::integer($row['use_count']),
        ], $rows);
    }

    public function lessons(array $ids): array
    {
        return $this->schedule->lessonsById($ids);
    }

    /**
     * @param array<string,mixed> $row
     * @return PassDocument
     */
    private function pass(array $row): array
    {
        $result = [
            'id' => $this->id($row['id']),
            'passType' => ['id' => $this->id($row['pass_type_id']), 'name' => self::text($row['type_name'])],
            'owner' => $this->person($row['person_id'], $row['owner_name'], $row['owner_anonymized_at']),
            'kind' => self::text($row['kind']), 'visitsTotal' => self::integer($row['visits_total']),
            'visitsRemaining' => self::integer($row['visits_remaining']), 'validFrom' => self::text($row['valid_from']),
            'status' => self::text($row['status']), 'origin' => self::text($row['origin']),
            'soldAt' => self::instant($row['sold_at']), 'pricePaid' => self::integer($row['price_paid']),
        ];
        if ($row['valid_until'] !== null) {
            $result['validUntil'] = self::text($row['valid_until']);
        }
        if ($row['voided_at'] !== null) {
            $result['voidedAt'] = self::instant($row['voided_at']);
        }
        if ($row['receipt_id'] !== null) {
            $result['receiptId'] = $this->id($row['receipt_id']);
        }
        if ($row['payer_person_id'] !== null) {
            $result['payer'] = $this->person($row['payer_person_id'], $row['payer_name'], $row['payer_anonymized_at']);
        }
        if ($row['note'] !== null && $row['note'] !== '') {
            $result['note'] = self::text($row['note']);
        }

        return $result;
    }

    /** @return PersonRef */
    private function person(mixed $id, mixed $name, mixed $anonymized): array
    {
        $person = ['id' => $this->id($id), 'name' => self::text($name)];
        if ($anonymized !== null) {
            $person['anonymized'] = true;
        }

        return $person;
    }

    private function ownerScope(bool $family): string
    {
        return $family
            ? '(p.person_id = ? OR EXISTS (SELECT 1 FROM person_guardians g WHERE g.guardian_id = ? AND g.dependent_id = p.person_id))'
            : 'p.person_id = ?';
    }

    /** @return list<string> */
    private function ownerKeys(string $person, bool $family): array
    {
        return array_fill(0, $family ? 2 : 1, $this->key($person));
    }

    private function keyType(): ParameterType
    {
        return $this->isPostgres() ? ParameterType::STRING : ParameterType::BINARY;
    }

    private function key(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    private function id(mixed $id): string
    {
        $value = self::text($id);

        return $this->isPostgres() ? $value : Uuid::fromBinary($value)->toRfc4122();
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private static function text(mixed $value): string
    {
        return is_string($value) ? $value : throw new RuntimeException('Expected a pass text column');
    }

    private static function integer(mixed $value): int
    {
        return is_numeric($value) ? (int) $value : throw new RuntimeException('Expected a pass integer column');
    }

    private static function instant(mixed $value): string
    {
        return Instant::format(Database::instant(self::text($value)));
    }
}
