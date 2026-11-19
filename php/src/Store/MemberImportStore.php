<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Service\MemberImportPersistence;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type ImportPerson from MemberImportPersistence
 * @phpstan-import-type Counts from MemberImportPersistence
 * @phpstan-import-type RunError from MemberImportPersistence
 */
final readonly class MemberImportStore implements MemberImportPersistence
{
    public function __construct(private Connection $db) {}

    /**
     * @template T
     * @param Closure():T $write
     * @return T
     */
    public function write(Closure $write): mixed
    {
        for ($attempt = 1; ; ++$attempt) {
            try {
                return Database::retryWrite($this->db, function () use ($write): mixed {
                    if ($this->isPostgres()) {
                        $this->db->executeStatement("SET LOCAL DateStyle TO 'ISO, YMD'");
                    }

                    return $write();
                }, readCommitted: true);
            } catch (UniqueConstraintViolationException $error) {
                // A concurrent import can insert a member number after the directory read.
                if ($attempt >= 3 || !str_contains($error->getMessage(), 'persons_member_no_key')) {
                    throw $error;
                }
            }
        }
    }

    /** @return list<ImportPerson> */
    public function people(): array
    {
        $order = $this->isPostgres() ? 'p.family_name, p.given_name' : 'CAST(p.family_name AS BINARY), CAST(p.given_name AS BINARY)';
        /** @var list<array{id:string,member_no:?string,given_name:string,family_name:string,email:?string,phone:?string,birth_date:?string,notes:?string,account_email:?string}> $rows */
        $rows = $this->db->fetchAllAssociative(
            'SELECT p.id, p.member_no, p.given_name, p.family_name, p.email, p.phone, p.birth_date, p.notes, a.email AS account_email'
            . ' FROM persons p LEFT JOIN accounts a ON a.person_id = p.id WHERE p.merged_into IS NULL AND p.anonymized_at IS NULL'
            . ' ORDER BY ' . $order . ', p.id',
        );

        return array_map(fn(array $row): array => [
            'id' => $this->decode($row['id']), 'memberNo' => $row['member_no'] ?? '',
            'givenName' => $row['given_name'], 'familyName' => $row['family_name'], 'email' => $row['email'] ?? '',
            'phone' => $row['phone'] ?? '', 'birthDate' => $row['birth_date'] ?? '', 'notes' => $row['notes'] ?? '',
            'accountEmail' => $row['account_email'] ?? '',
        ], $rows);
    }

    /** @return list<ImportPerson> */
    public function searchPeople(): array
    {
        return Database::inReadSnapshot($this->db, $this->people(...));
    }

    /**
     * @param list<string> $ids
     * @return list<string> Locked rows still present in the database.
     */
    public function lockPeople(array $ids): array
    {
        sort($ids, SORT_STRING);
        $locked = [];
        foreach (array_chunk($ids, 256) as $batch) {
            $bind = implode(', ', array_fill(0, count($batch), '?'));
            $rows = $this->db->fetchFirstColumn(
                "SELECT id FROM persons WHERE id IN ($bind) ORDER BY id FOR UPDATE",
                array_map($this->encode(...), $batch),
            );
            foreach ($rows as $id) {
                assert(is_string($id));
                $locked[] = $this->decode($id);
            }
        }

        return $locked;
    }

    public function schoolZone(): DateTimeZone
    {
        $zone = $this->db->fetchOne('SELECT timezone FROM settings WHERE id = TRUE');

        return new DateTimeZone(is_string($zone) ? $zone : 'UTC');
    }

    /** @param ImportPerson $person */
    public function save(array $person, bool $create): void
    {
        $id = $this->encode($person['id']);
        $values = [
            'given_name' => $person['givenName'], 'family_name' => $person['familyName'],
            'email' => self::nullable($person['email']), 'phone' => self::nullable($person['phone']),
            'birth_date' => self::nullable($person['birthDate']), 'notes' => self::nullable($person['notes']),
            'member_no' => self::nullable($person['memberNo']),
        ];
        if ($create) {
            $this->db->insert('persons', ['id' => $id] + $values);
        } else {
            $this->db->update('persons', $values, ['id' => $id]);
        }
    }

    public function linked(string $guardian, string $member): bool
    {
        return $this->db->fetchOne(
            'SELECT 1 FROM person_guardians WHERE guardian_id = ? AND dependent_id = ?',
            [$this->encode($guardian), $this->encode($member)],
        ) !== false;
    }

    public function link(string $guardian, string $member): void
    {
        $this->db->insert('person_guardians', ['guardian_id' => $this->encode($guardian), 'dependent_id' => $this->encode($member)]);
    }

    /** @return list<string> */
    public function dependents(string $person): array
    {
        $order = $this->isPostgres() ? 'p.family_name, p.given_name' : 'CAST(p.family_name AS BINARY), CAST(p.given_name AS BINARY)';
        $rows = $this->db->fetchFirstColumn(
            'SELECT p.id FROM person_guardians g JOIN persons p ON p.id = g.dependent_id WHERE g.guardian_id = ? ORDER BY ' . $order . ', p.id',
            [$this->encode($person)],
        );

        return array_map($this->decode(...), array_values(array_filter($rows, is_string(...))));
    }

    /**
     * @param Counts $counts
     * @param list<RunError> $errors
     */
    public function record(string $name, ?string $actor, DateTimeImmutable $start, DateTimeImmutable $end, array $counts, array $errors): string
    {
        $id = Uuid::v7()->toRfc4122();
        $this->db->insert('import_runs', [
            'id' => $this->encode($id), 'file_name' => $name,
            'actor_id' => $actor === null ? null : $this->encode($actor),
            'started_at' => Database::sqlInstant($this->db, $start), 'finished_at' => Database::sqlInstant($this->db, $end),
            'rows_total' => $counts['rows'], 'created' => $counts['created'], 'updated' => $counts['updated'],
            'unchanged' => $counts['unchanged'], 'failed' => $counts['failed'],
            'guardians_created' => $counts['guardiansCreated'], 'guardians_updated' => $counts['guardiansUpdated'],
            'links_added' => $counts['linksAdded'], 'errors' => $errors === [] ? null : json_encode($errors, JSON_THROW_ON_ERROR),
        ]);

        return $id;
    }

    /** @return list<array<string,mixed>> */
    public function runs(): array
    {
        return Database::inReadSnapshot($this->db, function (): array {
            /** @var list<array{id:string,file_name:string,actor_id:?string,actor_email:?string,given_name:?string,family_name:?string,started_at:string,finished_at:string,errors:?string,rows_total:int|numeric-string,created:int|numeric-string,updated:int|numeric-string,unchanged:int|numeric-string,failed:int|numeric-string,guardians_created:int|numeric-string,guardians_updated:int|numeric-string,links_added:int|numeric-string}> $rows */
            $rows = $this->db->fetchAllAssociative(
                'SELECT r.*, a.email AS actor_email, p.given_name, p.family_name FROM import_runs r'
                . ' LEFT JOIN accounts a ON a.id = r.actor_id LEFT JOIN persons p ON p.id = a.person_id'
                . ' ORDER BY r.started_at DESC, r.id DESC LIMIT 50',
            );
            $items = [];
            foreach ($rows as $row) {
                $item = [
                    'id' => $this->decode($row['id']), 'fileName' => $row['file_name'],
                    'startedAt' => Instant::format(Database::instant($row['started_at'])),
                    'finishedAt' => Instant::format(Database::instant($row['finished_at'])),
                    'counts' => [
                        'rows' => (int) $row['rows_total'], 'created' => (int) $row['created'],
                        'updated' => (int) $row['updated'], 'unchanged' => (int) $row['unchanged'], 'failed' => (int) $row['failed'],
                        'guardiansCreated' => (int) $row['guardians_created'], 'guardiansUpdated' => (int) $row['guardians_updated'],
                        'linksAdded' => (int) $row['links_added'],
                    ],
                    'errors' => $row['errors'] === null ? [] : json_decode($row['errors'], true, flags: JSON_THROW_ON_ERROR),
                ];
                if ($row['actor_id'] !== null) {
                    $item['by'] = ['id' => $this->decode($row['actor_id'])] + array_filter([
                        'email' => $row['actor_email'], 'givenName' => $row['given_name'], 'familyName' => $row['family_name'],
                    ], static fn(?string $value): bool => $value !== null && $value !== '');
                }
                $items[] = $item;
            }

            return $items;
        });
    }

    private static function nullable(string $value): ?string
    {
        return $value === '' ? null : $value;
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private function encode(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    private function decode(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromBinary($id)->toRfc4122();
    }
}
