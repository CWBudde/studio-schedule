<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\PersonData;
use StudioSchedule\Service\SettingsNotFound;
use Symfony\Component\Uid\Uuid;

final readonly class FamilyStore
{
    private const string PERSON_COLUMNS = 'p.id, p.given_name, p.family_name, p.phone, p.birth_date';
    private const string MANAGED = <<<'SQL'
        CASE WHEN EXISTS (SELECT 1 FROM accounts a WHERE a.person_id = p.id)
                OR EXISTS (SELECT 1 FROM staff s WHERE s.person_id = p.id)
                OR EXISTS (SELECT 1 FROM person_guardians d WHERE d.guardian_id = p.id)
             THEN 1 ELSE 0 END
        SQL;

    public function __construct(private Connection $db) {}

    /**
     * @template T
     * @param Closure():T $read
     * @return T
     */
    public function inReadSnapshot(Closure $read): mixed
    {
        return Database::inReadSnapshot($this->db, $read);
    }

    /**
     * @template T
     * @param Closure():T $write
     * @return T
     */
    public function inTransaction(Closure $write): mixed
    {
        return Database::retryWrite($this->db, $write);
    }

    /** @return array<string,mixed>|null */
    public function person(string $id): ?array
    {
        $row = $this->db->fetchAssociative(
            'SELECT ' . self::PERSON_COLUMNS . ' FROM persons p WHERE p.id = ' . $this->idParameter(),
            [$this->idValue($id)],
        );

        return $row === false ? null : $this->view($row);
    }

    /** @return list<array<string,mixed>> */
    public function dependents(string $guardian): array
    {
        $key = $this->idValue($guardian);
        $parameter = $this->idParameter();
        $order = $this->isPostgres() ? 'p.family_name, p.given_name, p.id' : 'CAST(p.family_name AS BINARY), CAST(p.given_name AS BINARY), p.id';
        $rows = $this->db->fetchAllAssociative(
            'SELECT ' . self::PERSON_COLUMNS . ', ' . self::MANAGED . ' AS managed FROM person_guardians g '
            . "JOIN persons p ON p.id = g.dependent_id WHERE g.guardian_id = $parameter ORDER BY $order",
            [$key],
        );
        if ($rows === []) {
            return [];
        }

        $otherGuardians = $this->db->fetchAllAssociative(
            'SELECT family.dependent_id, p.given_name, p.family_name FROM person_guardians family '
            . 'JOIN person_guardians other ON other.dependent_id = family.dependent_id '
            . "JOIN persons p ON p.id = other.guardian_id WHERE family.guardian_id = $parameter AND p.id <> $parameter ORDER BY $order",
            [$key, $key],
        );
        $names = [];
        foreach ($otherGuardians as $row) {
            $names[$this->id($row['dependent_id'])][] = ['givenName' => self::text($row['given_name']), 'familyName' => self::text($row['family_name'])];
        }

        return array_map(fn(array $row): array => $this->view($row) + [
            'guardians' => $names[$this->id($row['id'])] ?? [],
            'managed' => in_array($row['managed'], [1, '1'], true),
        ], $rows);
    }

    public function lockLivePersons(string ...$ids): bool
    {
        sort($ids, SORT_STRING);
        $sql = 'SELECT id FROM persons WHERE id = ' . $this->idParameter() . ' AND merged_into IS NULL AND anonymized_at IS NULL FOR UPDATE';
        foreach ($ids as $id) {
            if ($this->db->fetchOne($sql, [$this->idValue($id)]) === false) {
                return false;
            }
        }

        return true;
    }

    public function isGuardian(string $guardian, string $dependent, bool $forUpdate = false): bool
    {
        $parameter = $this->idParameter();

        return $this->db->fetchOne(
            "SELECT 1 FROM person_guardians WHERE guardian_id = $parameter AND dependent_id = $parameter" . ($forUpdate ? ' FOR UPDATE' : ''),
            [$this->idValue($guardian), $this->idValue($dependent)],
        ) !== false;
    }

    public function managed(string $id): bool
    {
        return in_array($this->db->fetchOne(
            'SELECT ' . self::MANAGED . ' FROM persons p WHERE p.id = ' . $this->idParameter(),
            [$this->idValue($id)],
        ), [1, '1'], true);
    }

    public function schoolTimezone(): string
    {
        $zone = $this->db->fetchOne('SELECT timezone FROM settings WHERE id = TRUE');
        if ($zone === false) {
            throw new SettingsNotFound();
        }

        return self::text($zone);
    }

    public function updatePerson(string $id, #[SensitiveParameter] PersonData $data): void
    {
        $this->db->executeStatement(
            'UPDATE persons SET given_name = ?, family_name = ?, phone = ?, birth_date = ? WHERE id = ' . $this->idParameter(),
            [...self::fields($data), $this->idValue($id)],
        );
    }

    public function createDependent(string $id, string $guardian, #[SensitiveParameter] PersonData $data): void
    {
        $parameter = $this->idParameter();
        $this->db->executeStatement(
            "INSERT INTO persons (id, given_name, family_name, phone, birth_date) VALUES ($parameter, ?, ?, ?, ?)",
            [$this->idValue($id), ...self::fields($data)],
        );
        $this->db->executeStatement(
            "INSERT INTO person_guardians (guardian_id, dependent_id) VALUES ($parameter, $parameter)",
            [$this->idValue($guardian), $this->idValue($id)],
        );
    }

    /**
     * @param array<string,mixed> $row
     * @return array<string,mixed>
     */
    private function view(#[SensitiveParameter] array $row): array
    {
        return (new PersonData(
            self::text($row['given_name']),
            self::text($row['family_name']),
            $row['phone'] === null ? '' : self::text($row['phone']),
            Date::fromText($row['birth_date'] === null ? '' : self::text($row['birth_date'])),
        ))->view($this->id($row['id']));
    }

    /** @return array{string,string,?string,?string} */
    private static function fields(#[SensitiveParameter] PersonData $data): array
    {
        return [
            $data->givenName, $data->familyName,
            $data->phone === '' ? null : $data->phone,
            $data->birthDate->isZero() ? null : (string) $data->birthDate,
        ];
    }

    private function idParameter(): string
    {
        return $this->isPostgres() ? '?' : 'UNHEX(?)';
    }

    private function idValue(string $id): string
    {
        return $this->isPostgres() ? $id : str_replace('-', '', $id);
    }

    private function id(mixed $value): string
    {
        $id = self::text($value);

        return $this->isPostgres() ? $id : Uuid::fromBinary($id)->toRfc4122();
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private static function text(mixed $value): string
    {
        return is_string($value) ? $value : throw new RuntimeException('Expected a person text column');
    }
}
