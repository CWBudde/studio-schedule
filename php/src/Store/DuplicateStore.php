<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use Psr\Clock\ClockInterface;
use StudioSchedule\Service\DuplicatePersistence;
use Symfony\Component\Uid\Uuid;

final readonly class DuplicateStore implements DuplicatePersistence
{
    public function __construct(
        private Connection $db,
        private MemberImportStore $directory,
        private FamilyStore $families,
        private ClockInterface $clock,
    ) {}

    public function snapshot(): array
    {
        return Database::inReadSnapshot($this->db, function (): array {
            $people = $this->directory->people();
            /** @var list<array{dependent_id:string,guardian_id:string}> $links */
            $links = $this->db->fetchAllAssociative(
                'SELECT g.dependent_id, g.guardian_id FROM person_guardians g JOIN persons p ON p.id = g.dependent_id '
                . 'WHERE p.merged_into IS NULL AND p.anonymized_at IS NULL',
            );
            $guardians = [];
            foreach ($links as $link) {
                $guardians[$this->decode($link['dependent_id'])][$this->decode($link['guardian_id'])] = true;
            }
            /** @var list<array{person_a:string,person_b:string}> $pairs */
            $pairs = $this->db->fetchAllAssociative('SELECT person_a, person_b FROM person_duplicate_dismissals');
            $dismissed = [];
            foreach ($pairs as $pair) {
                $dismissed[$this->decode($pair['person_a'])][$this->decode($pair['person_b'])] = true;
            }

            return ['people' => $people, 'guardians' => $guardians, 'dismissed' => $dismissed];
        });
    }

    public function saveDismissal(string $a, string $b, string $actor): bool
    {
        return Database::retryWrite($this->db, function () use ($a, $b, $actor): bool {
            if (!$this->families->lockLivePersons($a, $b)) {
                return false;
            }
            $insert = 'INSERT INTO person_duplicate_dismissals (person_a, person_b, dismissed_by, dismissed_at) VALUES (?, ?, ?, ?)';
            $onConflict = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform
                ? ' ON CONFLICT (person_a, person_b) DO NOTHING'
                : ' ON DUPLICATE KEY UPDATE person_a = person_a';
            $this->db->executeStatement($insert . $onConflict, [
                $this->encode($a), $this->encode($b), $this->encode($actor), Database::sqlInstant($this->db, $this->clock->now()),
            ]);

            return true;
        }, readCommitted: true);
    }

    private function encode(string $id): string
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform ? $id : Uuid::fromString($id)->toBinary();
    }

    private function decode(string $id): string
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform ? $id : Uuid::fromBinary($id)->toRfc4122();
    }
}
