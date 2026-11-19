<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use SensitiveParameter;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Mail\Formatting;
use StudioSchedule\Mail\Rendered;
use StudioSchedule\Service\LessonNoticePersistence;
use Symfony\Component\Uid\Uuid;

final readonly class LessonNoticeStore implements LessonNoticePersistence
{
    public function __construct(
        private Connection $db,
        private EventStore $transactions,
        private LessonWriteStore $lessons,
        private ScheduleStore $schedule,
    ) {}

    public function write(Closure $write): mixed
    {
        return $this->transactions->write($write);
    }

    public function lockLesson(string $id): ?PublishedLesson
    {
        return $this->lessons->lock($id) ? $this->schedule->lesson($id) : null;
    }

    public function activeStaff(string $person): array
    {
        return $this->schedule->activeStaff($person);
    }

    public function lockSender(string $account): ?array
    {
        // Match Go's lesson then sender-person lock order. The same person lock serializes the sender's hourly quota.
        $type = $this->idType();
        $key = $this->encodeId($account);
        /** @var array{id:string,given_name:string,family_name:string}|false $person */
        $person = $this->db->fetchAssociative(
            'SELECT id, given_name, family_name FROM persons WHERE id = (SELECT person_id FROM accounts WHERE id = ?) FOR UPDATE',
            [$key],
            [$type],
        );
        if ($person === false) {
            return null;
        }
        /** @var string|false $email */
        $email = $this->db->fetchOne('SELECT email FROM accounts WHERE id = ? AND person_id = ?', [$key, $person['id']], [$type, $type]);
        if ($email === false) {
            return null;
        }

        return [
            'personId' => $this->isPostgres() ? $person['id'] : Uuid::fromBinary($person['id'])->toRfc4122(),
            'name' => Formatting::trimWhitespace($person['given_name'] . ' ' . $person['family_name']),
            'email' => $email,
        ];
    }

    public function senderNoticeAtOffset(string $account, DateTimeImmutable $since, int $offset): ?DateTimeImmutable
    {
        /** @var string|false $sent */
        $sent = $this->db->fetchOne(
            'SELECT sent_at FROM lesson_notices WHERE sender_id = ? AND sent_at >= ? ORDER BY sent_at DESC, id DESC LIMIT 1 OFFSET ?',
            [$this->encodeId($account), Database::sqlInstant($this->db, $since), $offset],
            [$this->idType(), ParameterType::STRING, ParameterType::INTEGER],
        );

        return $sent === false ? null : Database::instant($sent);
    }

    public function lessonNoticeCount(string $lesson, DateTimeImmutable $since): int
    {
        /** @var int|numeric-string $count */
        $count = $this->db->fetchOne(
            'SELECT COUNT(*) FROM lesson_notices WHERE lesson_id = ? AND sent_at >= ?',
            [$this->encodeId($lesson), Database::sqlInstant($this->db, $since)],
            [$this->idType(), ParameterType::STRING],
        );

        return (int) $count;
    }

    public function enqueue(#[SensitiveParameter] string $recipient, #[SensitiveParameter] string $replyTo, #[SensitiveParameter] Rendered $mail, DateTimeImmutable $now): void
    {
        (new OutboxStore($this->db))->enqueue($recipient, 'lesson-notice', $mail, $now, replyTo: $replyTo);
    }

    public function record(string $lesson, string $sender, DateTimeImmutable $now, int $recipients): void
    {
        $type = $this->idType();
        $this->db->executeStatement(
            'INSERT INTO lesson_notices (id, lesson_id, sender_id, sent_at, recipients) VALUES (?, ?, ?, ?, ?)',
            [$this->encodeId(Uuid::v7()->toRfc4122()), $this->encodeId($lesson), $this->encodeId($sender), Database::sqlInstant($this->db, $now), $recipients],
            [$type, $type, $type, ParameterType::STRING, ParameterType::INTEGER],
        );
    }

    private function idType(): ParameterType
    {
        return $this->isPostgres() ? ParameterType::STRING : ParameterType::BINARY;
    }

    private function encodeId(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
