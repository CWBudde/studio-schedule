<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use StudioSchedule\Service\WaitlistInviteReader;
use Symfony\Component\Uid\Uuid;

final readonly class WaitlistInviteStore implements WaitlistInviteReader
{
    public function __construct(private Connection $db, private ScheduleStore $schedule) {}

    public function lookup(string $tokenHash): ?array
    {
        return Database::inReadSnapshot($this->db, function () use ($tokenHash): ?array {
            $postgres = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
            $hash = $postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
            /** @var array{given_name:string,status:string,expires_at:?string,lesson_id:string,position:int|numeric-string}|false $row */
            $row = $this->db->fetchAssociative(
                'SELECT p.given_name, w.status, w.expires_at, w.lesson_id, '
                . "CASE WHEN w.status = 'waiting' THEN (SELECT COUNT(*) FROM waitlist_entries ahead "
                . "WHERE ahead.lesson_id = w.lesson_id AND ahead.status = 'waiting' "
                . 'AND (ahead.joined_at < w.joined_at OR (ahead.joined_at = w.joined_at AND ahead.id <= w.id))) ELSE 0 END AS position '
                . "FROM waitlist_entries w JOIN persons p ON p.id = w.person_id WHERE w.invite_token_hash = $hash",
                [$tokenHash],
            );
            if ($row === false) {
                return null;
            }

            $id = $postgres ? $row['lesson_id'] : Uuid::fromBinary($row['lesson_id'])->toRfc4122();
            $lesson = $this->schedule->lessonsById([$id])[0] ?? null;

            return $lesson === null ? null : [
                'givenName' => $row['given_name'],
                'status' => $row['status'],
                'expiresAt' => $row['expires_at'] === null ? null : Database::instant($row['expires_at']),
                'position' => (int) $row['position'],
                'lesson' => $lesson,
                'rules' => $this->schedule->rules(),
            ];
        });
    }
}
