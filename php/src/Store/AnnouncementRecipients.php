<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use StudioSchedule\Domain\AnnouncementAudience;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-type Recipient array{email:string,locale:string,givenName:string,participants:list<string>}
 * @phpstan-type RecipientRow array{email:?string,locale:?string,given_name:?string,participant:?string,from_account:bool|int|string}
 */
final readonly class AnnouncementRecipients
{
    public function __construct(private Connection $db) {}

    /** @return array<string,Recipient> Recipients keyed and ordered by their lowercased addresses. */
    public function find(AnnouncementAudience $audience, DateTimeImmutable $now, string $timezone): array
    {
        if ($audience->reference === null) {
            $rows = $this->accounts($audience->role);
        } else {
            $id = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform ? $audience->reference : Uuid::fromString($audience->reference)->toBinary();
            $rows = $this->forReference($audience->kind, $id, $now, $timezone);
        }

        $recipients = $accountAddresses = [];
        foreach ($rows as $row) {
            $email = $row['email'];
            if ($email === null || $email === '') {
                continue;
            }
            $key = mb_convert_case($email, MB_CASE_LOWER_SIMPLE, 'UTF-8');
            $fromAccount = AnnouncementStore::boolean($row['from_account']);
            if (!isset($recipients[$key]) || ($fromAccount && !isset($accountAddresses[$key]))) {
                $recipients[$key] = [
                    'email' => $email, 'locale' => $row['locale'] ?? '', 'givenName' => $row['given_name'] ?? '',
                    'participants' => $recipients[$key]['participants'] ?? [],
                ];
            }
            if ($fromAccount) {
                $accountAddresses[$key] = true;
            }
            if ($row['participant'] !== null && !in_array($row['participant'], $recipients[$key]['participants'], true)) {
                $recipients[$key]['participants'][] = $row['participant'];
            }
        }
        ksort($recipients, SORT_STRING);

        return $recipients;
    }

    /** @return list<RecipientRow> */
    private function accounts(?string $role): array
    {
        $query = 'SELECT a.email, a.locale, p.given_name, NULL AS participant, TRUE AS from_account'
            . ' FROM accounts a JOIN persons p ON p.id = a.person_id WHERE a.confirmed_at IS NOT NULL AND a.disabled_at IS NULL';
        /** @var list<RecipientRow> $rows */
        $rows = $role === null
            ? $this->db->fetchAllAssociative($query . ' ORDER BY a.email, a.id')
            : $this->db->fetchAllAssociative($query . ' AND a.role = ? ORDER BY a.email, a.id', [$role]);

        return $rows;
    }

    /** @return list<RecipientRow> */
    private function forReference(string $kind, string $id, DateTimeImmutable $now, string $timezone): array
    {
        $event = $kind === 'event' ? $id : null;
        if ($kind === 'lesson') {
            $parent = $this->db->fetchOne('SELECT event_id FROM lessons WHERE id = ?', [$id]);
            $event = is_string($parent) ? $parent : null;
        }
        $eventPeople = "SELECT person_id FROM event_registrations WHERE event_id = ? AND status = 'registered' AND person_id IS NOT NULL";
        if ($event !== null) {
            /** @var list<RecipientRow> $guests */
            $guests = $this->db->fetchAllAssociative(
                'SELECT guest_email AS email, NULL AS locale, guest_name AS given_name, guest_name AS participant, FALSE AS from_account'
                . " FROM event_registrations WHERE event_id = ? AND status = 'registered' AND person_id IS NULL"
                . ' AND guest_email IS NOT NULL ORDER BY created_at, id',
                [$event],
            );

            return [...$guests, ...$this->personContacts($eventPeople, [$event])];
        }

        $sql = "SELECT b.person_id FROM bookings b JOIN lessons l ON l.id = b.lesson_id WHERE b.status <> 'canceled' AND b.person_id IS NOT NULL AND ";
        $at = Database::sqlInstant($this->db, $now);
        $today = $now->setTimezone(new DateTimeZone($timezone))->format('Y-m-d');
        [$people, $params] = match ($kind) {
            'lesson' => [$sql . 'l.id = ?', [$id]],
            'class' => [
                'SELECT person_id FROM class_members WHERE class_id = ? UNION ' . $sql . 'l.class_id = ? AND l.ends_at > ?',
                [$id, $id, $at],
            ],
            'course-type' => [
                'SELECT m.person_id FROM class_members m JOIN classes c ON c.id = m.class_id JOIN terms t ON t.id = c.term_id'
                . ' WHERE c.course_type_id = ? AND t.ends_on >= ? UNION ' . $sql . 'l.course_type_id = ? AND l.ends_at > ?',
                [$id, $today, $id, $at],
            ],
            default => throw new \LogicException('Unknown announcement reference'),
        };

        return $this->personContacts($people, $params);
    }

    /**
     * @param list<string> $params
     * @return list<RecipientRow>
     */
    private function personContacts(string $people, array $params): array
    {
        $sql = <<<'SQL'
            SELECT CASE WHEN a.confirmed_at IS NOT NULL THEN a.email ELSE COALESCE(NULLIF(p.email, ''), ga.email) END AS email,
                CASE WHEN a.confirmed_at IS NOT NULL THEN a.locale ELSE ga.locale END AS locale,
                COALESCE(gp.given_name, p.given_name) AS given_name, CONCAT(p.given_name, ' ', p.family_name) AS participant,
                (a.confirmed_at IS NOT NULL OR ga.id IS NOT NULL) AS from_account
            FROM (PEOPLE) selected
            JOIN persons p ON p.id = selected.person_id
            LEFT JOIN accounts a ON a.person_id = p.id
            LEFT JOIN person_guardians g ON g.dependent_id = p.id AND a.confirmed_at IS NULL AND NULLIF(p.email, '') IS NULL
            LEFT JOIN accounts ga ON ga.person_id = g.guardian_id AND ga.confirmed_at IS NOT NULL AND ga.disabled_at IS NULL
            LEFT JOIN persons gp ON gp.id = ga.person_id
            WHERE a.disabled_at IS NULL
            ORDER BY p.family_name, p.given_name, p.id, gp.family_name, gp.given_name, gp.id
            SQL;
        /** @var list<RecipientRow> $rows */
        $rows = $this->db->fetchAllAssociative(str_replace('PEOPLE', $people, $sql), $params);

        return $rows;
    }
}
