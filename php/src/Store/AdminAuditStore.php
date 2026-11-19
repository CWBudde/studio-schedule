<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use stdClass;
use StudioSchedule\Domain\Id;
use StudioSchedule\Domain\Instant;
use Symfony\Component\Uid\Uuid;

final readonly class AdminAuditStore
{
    private const int LIMIT = 200;

    public function __construct(private Connection $db) {}

    /** @return list<array<string, mixed>> */
    public function list(): array
    {
        $postgres = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;

        return Database::inReadSnapshot($this->db, function () use ($postgres): array {
            /** @var list<array{
             *     id:string,occurred_at:string,action:string,details:?string,
             *     actor_id:?string,actor_email:?string,actor_given:?string,actor_family:?string,
             *     target_id:?string,target_email:?string,target_given:?string,target_family:?string,
             *     target_person_id:?string,person_given:?string,person_family:?string,anonymized_at:?string
             * }> $rows
             */
            $rows = $this->db->fetchAllAssociative(
                'SELECT e.id, e.occurred_at, e.action, e.details, e.actor_id, e.target_id, e.target_person_id, '
                . 'a.email AS actor_email, ap.given_name AS actor_given, ap.family_name AS actor_family, '
                . 't.email AS target_email, tp.given_name AS target_given, tp.family_name AS target_family, '
                . 'p.given_name AS person_given, p.family_name AS person_family, p.anonymized_at '
                . 'FROM audit_events e '
                . 'LEFT JOIN accounts a ON a.id = e.actor_id LEFT JOIN persons ap ON ap.id = a.person_id '
                . 'LEFT JOIN accounts t ON t.id = e.target_id LEFT JOIN persons tp ON tp.id = t.person_id '
                . 'LEFT JOIN persons p ON p.id = e.target_person_id '
                . 'ORDER BY e.occurred_at DESC, e.id DESC LIMIT ' . self::LIMIT,
            );

            $items = [];
            foreach ($rows as $row) {
                $entry = [
                    'id' => self::id($row['id'], $postgres),
                    'at' => Instant::format(Database::instant($row['occurred_at'])),
                    'action' => $row['action'],
                ];
                if ($row['actor_id'] !== null) {
                    $entry['actor'] = self::name(self::id($row['actor_id'], $postgres), $row['actor_given'], $row['actor_family'], $row['actor_email']);
                }
                if ($row['target_id'] !== null) {
                    $entry['target'] = self::name(self::id($row['target_id'], $postgres), $row['target_given'], $row['target_family'], $row['target_email']);
                }
                if ($row['target_person_id'] !== null) {
                    $person = self::name(self::id($row['target_person_id'], $postgres), $row['person_given'], $row['person_family']);
                    $entry['targetPerson'] = $row['anonymized_at'] === null ? $person : $person + ['anonymized' => true];
                }
                $details = self::details($row['details']);
                if ($details !== []) {
                    $entry['details'] = $details;
                }
                $items[] = $entry;
            }

            return $items;
        });
    }

    /** @return array<string, string> */
    private static function name(string $id, ?string $given, ?string $family, ?string $email = null): array
    {
        return array_filter(
            ['id' => $id, 'email' => $email, 'givenName' => $given, 'familyName' => $family],
            static fn(?string $value): bool => $value !== null && $value !== '',
        );
    }

    /** @return array<string, mixed> */
    private static function details(?string $json): array
    {
        $stored = $json === null ? null : json_decode($json, flags: JSON_THROW_ON_ERROR);
        if (!$stored instanceof stdClass) {
            return [];
        }

        // Audit details are a typed API projection. Keep raw JSON and any unknown keys private.
        $details = [];
        foreach (['recipient', 'template', 'mergedName', 'previousRole', 'role', 'delivery', 'tool', 'outcome'] as $field) {
            $value = $stored->{$field} ?? null;
            if (is_string($value) && $value !== '') {
                $details[$field] = $value;
            }
        }
        foreach (['mailId', 'mergedPersonId', 'eventId', 'registrationId'] as $field) {
            $value = $stored->{$field} ?? null;
            if (is_string($value) && Id::valid($value)) {
                $details[$field] = strtolower($value);
            }
        }
        if (is_int($stored->sessions ?? null) && $stored->sessions !== 0) {
            $details['sessions'] = $stored->sessions;
        }
        if (($stored->refund ?? null) === true) {
            $details['refund'] = true;
        }
        $via = $stored->via ?? null;
        if ($via instanceof stdClass) {
            $channel = is_string($via->channel ?? null) ? $via->channel : '';
            $osUser = is_string($via->osUser ?? null) ? $via->osUser : '';
            if ($channel !== '' || $osUser !== '') {
                $details['via'] = ['channel' => $channel, 'osUser' => $osUser];
            }
        }

        return $details;
    }

    private static function id(string $id, bool $postgres): string
    {
        return $postgres ? $id : Uuid::fromBinary($id)->toRfc4122();
    }
}
