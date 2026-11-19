<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use StudioSchedule\Domain\AnnouncementAudience;
use StudioSchedule\Service\AnnouncementInput;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-type AnnouncementRow array{
 *   id:string,author_id:string,title:string,body:string,audience:string,role:?string,class_id:?string,
 *   course_type_id:?string,lesson_id:?string,event_id:?string,publish_at:?string,expires_at:?string,
 *   email:bool|int|string,emailed_at:?string,email_recipients:int|numeric-string,mail_cursor:?string,
 *   created_at:string,updated_at:string
 * }
 * @phpstan-type OfficeRow array{
 *   id:string,author_id:string,title:string,body:string,audience:string,role:?string,class_id:?string,
 *   course_type_id:?string,lesson_id:?string,event_id:?string,publish_at:?string,expires_at:?string,
 *   email:bool|int|string,emailed_at:?string,email_recipients:int|numeric-string,mail_cursor:?string,
 *   created_at:string,updated_at:string,
 *   author:string,event_title:?string,description_course_id:?string,description_course_name:?string,
 *   weekday:int|numeric-string|null,start_time:?string,term_id:?string,term_name:?string,
 *   starts_at:?string,ends_at:?string
 * }
 */
final readonly class AnnouncementStore
{
    public const array REFERENCE_COLUMNS = [
        'class' => 'class_id', 'course-type' => 'course_type_id', 'lesson' => 'lesson_id', 'event' => 'event_id',
    ];

    public function __construct(private Connection $db) {}

    /**
     * @template T
     * @param Closure():T $read
     * @return T
     */
    public function read(Closure $read): mixed
    {
        return Database::inReadSnapshot($this->db, $read);
    }

    /**
     * @template T
     * @param Closure():T $write
     * @return T
     */
    public function write(Closure $write): mixed
    {
        return Database::retryWrite($this->db, function () use ($write): mixed {
            if ($this->postgres()) {
                $this->db->executeStatement("SET LOCAL DateStyle TO 'ISO, YMD'");
            }

            return $write();
        }, readCommitted: true);
    }

    public function lockMailingLimit(): void
    {
        if ($this->postgres()) {
            // Same key and scope as the Go announcement repository.
            $this->db->fetchOne("SELECT pg_advisory_xact_lock(hashtext('announcement-mail'))");

            return;
        }

        $this->db->fetchOne('SELECT id FROM settings WHERE id = TRUE FOR UPDATE');
    }

    /** @return array{school_name:string,timezone:string,locale:string} */
    public function settings(): array
    {
        /** @var array{school_name:string,timezone:string,locale:string}|false $row */
        $row = $this->db->fetchAssociative('SELECT school_name, timezone, default_locale AS locale FROM settings WHERE id = TRUE');

        return $row === false ? throw new SettingsNotFound() : $row;
    }

    public function referenceKind(AnnouncementAudience $audience): string|false
    {
        $sql = match ($audience->kind) {
            'class' => "SELECT 'class' FROM classes WHERE id = ?",
            'course-type' => "SELECT 'course-type' FROM course_types WHERE id = ?",
            'lesson' => 'SELECT kind FROM lessons WHERE id = ?',
            'event' => "SELECT 'event' FROM events WHERE id = ?",
            default => throw new \LogicException('This audience has no reference'),
        };
        $value = $this->db->fetchOne($sql, [$this->encode($audience->reference)]);

        return is_string($value) ? $value : false;
    }

    /** @return list<OfficeRow> */
    public function officeList(?string $id = null): array
    {
        $sql = <<<'SQL'
            SELECT a.*, CONCAT(p.given_name, ' ', p.family_name) AS author, e.title AS event_title,
                ct.id AS description_course_id, ct.name AS description_course_name,
                c.weekday, c.start_time, t.id AS term_id, t.name AS term_name, l.starts_at, l.ends_at
            FROM announcements a
            JOIN accounts acc ON acc.id = a.author_id
            JOIN persons p ON p.id = acc.person_id
            LEFT JOIN classes c ON c.id = a.class_id
            LEFT JOIN terms t ON t.id = c.term_id
            LEFT JOIN lessons l ON l.id = a.lesson_id
            LEFT JOIN course_types ct ON ct.id = COALESCE(a.course_type_id, c.course_type_id, l.course_type_id)
            LEFT JOIN events e ON e.id = a.event_id
            SQL;
        /** @var list<OfficeRow> $rows */
        $rows = $id === null
            ? $this->db->fetchAllAssociative($sql . ' ORDER BY (a.publish_at IS NOT NULL), a.publish_at DESC, a.id DESC')
            : $this->db->fetchAllAssociative($sql . ' WHERE a.id = ?', [$this->encode($id)]);

        return $rows;
    }

    /** @return AnnouncementRow|null */
    public function lock(string $id): ?array
    {
        /** @var AnnouncementRow|false $row */
        $row = $this->db->fetchAssociative('SELECT * FROM announcements WHERE id = ? FOR UPDATE', [$this->encode($id)]);

        return $row === false ? null : $row;
    }

    /** @return list<array{id:string,title:string,body:string,publishedAt:string,expiresAt?:string,dismissed:bool}> */
    public function shown(SessionPrincipal $principal, DateTimeImmutable $now, string $today, ?string $id = null): array
    {
        $family = $this->encode($principal->personId);
        $at = Database::sqlInstant($this->db, $now);
        $query = <<<'SQL'
            WITH family AS (
                SELECT id AS person_id FROM persons WHERE id = ?
                UNION SELECT dependent_id FROM person_guardians WHERE guardian_id = ?
            ),
            memberships AS (
                SELECT c.id AS class_id, c.course_type_id, t.ends_on
                FROM family f JOIN class_members m ON m.person_id = f.person_id
                JOIN classes c ON c.id = m.class_id JOIN terms t ON t.id = c.term_id
            ),
            active_bookings AS (
                SELECT l.id AS lesson_id, l.class_id, l.course_type_id, l.ends_at
                FROM family f JOIN bookings b ON b.person_id = f.person_id AND b.status <> 'canceled'
                JOIN lessons l ON l.id = b.lesson_id
            ),
            registrations AS (
                SELECT r.event_id FROM family f JOIN event_registrations r ON r.person_id = f.person_id AND r.status = 'registered'
            )
            SELECT a.id, a.title, a.body, a.publish_at, a.expires_at, (d.announcement_id IS NOT NULL) AS dismissed
            FROM announcements a
            LEFT JOIN announcement_dismissals d ON d.announcement_id = a.id AND d.account_id = ?
            WHERE a.publish_at <= ? AND (a.expires_at IS NULL OR a.expires_at > ?) AND (
                a.audience = 'everyone' OR (a.audience = 'role' AND a.role = ?)
                OR (a.audience = 'class' AND (
                    EXISTS (SELECT 1 FROM memberships m WHERE m.class_id = a.class_id)
                    OR EXISTS (SELECT 1 FROM active_bookings b WHERE b.class_id = a.class_id AND b.ends_at > ?)
                ))
                OR (a.audience = 'course-type' AND (
                    EXISTS (SELECT 1 FROM memberships m WHERE m.course_type_id = a.course_type_id AND m.ends_on >= ?)
                    OR EXISTS (SELECT 1 FROM active_bookings b WHERE b.course_type_id = a.course_type_id AND b.ends_at > ?)
                ))
                OR (a.audience = 'lesson' AND (
                    EXISTS (SELECT 1 FROM active_bookings b WHERE b.lesson_id = a.lesson_id)
                    OR EXISTS (SELECT 1 FROM lessons l JOIN registrations r ON r.event_id = l.event_id WHERE l.id = a.lesson_id)
                ))
                OR (a.audience = 'event' AND EXISTS (SELECT 1 FROM registrations r WHERE r.event_id = a.event_id))
            )
            SQL;
        $params = [$family, $family, $this->encode($principal->accountId), $at, $at, $principal->role, $at, $today, $at];
        if ($id !== null) {
            $query .= ' AND a.id = ?';
            $params[] = $this->encode($id);
        }
        /** @var list<array{id:string,title:string,body:string,publish_at:string,expires_at:?string,dismissed:bool|int|string}> $rows */
        $rows = $this->db->fetchAllAssociative($query . ' ORDER BY a.publish_at DESC, a.id DESC', $params);

        return array_map(fn(array $row): array => [
            'id' => $this->decode($row['id']), 'title' => $row['title'], 'body' => $row['body'],
            'publishedAt' => Instant::format(Database::instant($row['publish_at'])), 'dismissed' => self::boolean($row['dismissed']),
        ] + ($row['expires_at'] === null ? [] : ['expiresAt' => Instant::format(Database::instant($row['expires_at']))]), $rows);
    }

    public function dismiss(string $id, string $account, DateTimeImmutable $now): void
    {
        $suffix = $this->postgres()
            ? 'ON CONFLICT (account_id, announcement_id) DO NOTHING'
            : 'ON DUPLICATE KEY UPDATE dismissed_at = dismissed_at';
        $this->db->executeStatement(
            'INSERT INTO announcement_dismissals (announcement_id, account_id, dismissed_at) VALUES (?, ?, ?) ' . $suffix,
            [$this->encode($id), $this->encode($account), Database::sqlInstant($this->db, $now)],
        );
    }

    public function mailLimitUsed(DateTimeImmutable $from, DateTimeImmutable $to, string $except): int
    {
        $start = Database::sqlInstant($this->db, $from);
        $end = Database::sqlInstant($this->db, $to);
        $count = $this->db->fetchOne(
            'SELECT (SELECT COUNT(*) FROM announcement_mailings WHERE sent_at >= ? AND sent_at < ?)'
            . ' + (SELECT COUNT(*) FROM announcements WHERE email = TRUE AND emailed_at IS NULL AND publish_at >= ? AND publish_at < ? AND id <> ?)',
            [$start, $end, $start, $end, $this->encode($except)],
        );
        assert(is_numeric($count));

        return (int) $count;
    }

    public function save(string $id, string $author, AnnouncementInput $input, ?DateTimeImmutable $publish, bool $email, DateTimeImmutable $now, bool $create): void
    {
        $data = [
            'title' => $input->title, 'body' => $input->body, 'audience' => $input->audience->kind, 'role' => $input->audience->role,
            'email' => $email, 'publish_at' => $this->timestamp($publish), 'expires_at' => $this->timestamp($input->expiresAt),
            'updated_at' => $this->timestamp($now),
        ];
        foreach (self::REFERENCE_COLUMNS as $kind => $column) {
            $data[$column] = $kind === $input->audience->kind ? $this->encode($input->audience->reference) : null;
        }
        $types = ['email' => ParameterType::BOOLEAN];
        if ($create) {
            $this->db->insert('announcements', $data + [
                'id' => $this->encode($id), 'author_id' => $this->encode($author),
                'created_at' => $this->timestamp($now), 'email_recipients' => 0,
            ], $types);
        } else {
            $this->db->update('announcements', $data, ['id' => $this->encode($id)], $types);
        }
    }

    public function delete(string $id): void
    {
        $this->db->delete('announcements', ['id' => $this->encode($id)]);
    }

    /** @return list<AnnouncementRow> */
    public function due(DateTimeImmutable $now): array
    {
        /** @var list<AnnouncementRow> $rows */
        $rows = $this->db->fetchAllAssociative(
            'SELECT * FROM announcements WHERE email = TRUE AND emailed_at IS NULL AND publish_at <= ?'
            . ' ORDER BY publish_at, id LIMIT 10 FOR UPDATE SKIP LOCKED',
            [Database::sqlInstant($this->db, $now)],
        );

        return $rows;
    }

    public function mailProgress(string $id, int $recipients, ?string $cursor, ?DateTimeImmutable $finished): void
    {
        $this->db->update('announcements', [
            'email_recipients' => $recipients, 'mail_cursor' => $cursor, 'emailed_at' => $this->timestamp($finished),
        ], ['id' => $this->encode($id)]);
    }

    public function recordMailing(string $id, string $author, DateTimeImmutable $now, int $recipients): void
    {
        $this->db->insert('announcement_mailings', [
            'id' => $this->encode(Uuid::v7()->toRfc4122()), 'announcement_id' => $this->encode($id),
            'sender_id' => $this->encode($author), 'sent_at' => $this->timestamp($now), 'recipients' => $recipients,
        ]);
    }

    public function decode(string $id): string
    {
        return $this->postgres() ? $id : Uuid::fromBinary($id)->toRfc4122();
    }

    public static function boolean(bool|int|string $value): bool
    {
        return in_array($value, [true, 1, '1', 't', 'true'], true);
    }

    private function timestamp(?DateTimeImmutable $instant): ?string
    {
        return $instant === null ? null : Database::sqlInstant($this->db, $instant);
    }

    private function encode(?string $id): ?string
    {
        return $id === null || $this->postgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    private function postgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
