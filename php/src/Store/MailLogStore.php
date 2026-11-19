<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Mail\TemplateData;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-type MailLogRow array{
 *     id:string,recipient:string,subject:string,template:?string,reply_to:?string,created_at:string,
 *     attempts:int|numeric-string,next_attempt_at:?string,sent_at:?string,last_error:?string,
 *     missing_body:bool|int|string,receipt_number:?string,body_text?:?string,body_html?:?string
 * }
 */
final readonly class MailLogStore
{
    private const string METADATA = 'o.id, o.recipient, o.subject, o.template, o.reply_to, o.created_at, o.attempts, '
        . "o.next_attempt_at, o.sent_at, o.last_error, (o.body_text IS NULL OR o.body_text = '') AS missing_body, r.number AS receipt_number";
    private const string SOURCE = ' FROM outbox_mails o LEFT JOIN receipts r ON r.id = o.receipt_id';

    public function __construct(private Connection $db) {}

    /**
     * @param array{createdAt:DateTimeImmutable,id:string}|null $after
     * @return list<MailLogRow>
     */
    public function search(string $status, string $template, #[SensitiveParameter] string $recipient, ?array $after, int $limit): array
    {
        $filters = [match ($status) {
            'sent' => 'o.sent_at IS NOT NULL',
            'pending' => 'o.sent_at IS NULL AND o.next_attempt_at IS NOT NULL',
            'failed' => 'o.sent_at IS NULL AND o.next_attempt_at IS NULL',
            default => '1 = 1',
        }];
        $parameters = [];
        if ($template !== '') {
            $filters[] = 'o.template = ?';
            $parameters[] = $template;
        }
        if ($recipient !== '') {
            $filters[] = Database::asciiContains('o.recipient', $this->isPostgres());
            $parameters[] = strtolower($recipient);
        }
        if ($after !== null) {
            $filters[] = '(o.created_at, o.id) < (?, ' . $this->idParameter() . ')';
            $parameters[] = $this->timestamp($after['createdAt']);
            $parameters[] = $this->idValue($after['id']);
        }

        return Database::inReadSnapshot($this->db, function () use ($filters, $parameters, $limit): array {
            /** @var list<MailLogRow> $rows */
            $rows = $this->db->fetchAllAssociative(
                'SELECT ' . self::METADATA . self::SOURCE . ' WHERE ' . implode(' AND ', $filters)
                . ' ORDER BY o.created_at DESC, o.id DESC LIMIT ' . $limit,
                $parameters,
            );

            return array_map($this->withTextId(...), $rows);
        });
    }

    /** @return MailLogRow|null */
    public function detail(string $id): ?array
    {
        return Database::inReadSnapshot($this->db, fn(): ?array => $this->find($id, includeBodies: true));
    }

    /** @return MailLogRow|'secret-link'|'not-failed'|null */
    public function requeue(string $id, string $actorId, DateTimeImmutable $now): array|string|null
    {
        return $this->db->transactional(function (Connection $db) use ($id, $actorId, $now): array|string|null {
            if ($this->isPostgres()) {
                $db->executeStatement("SET LOCAL DateStyle TO 'ISO, YMD'");
            }
            $param = $this->idParameter();
            $key = $this->idValue($id);
            /** @var array{recipient:string,template:?string,sent_at:?string,next_attempt_at:?string,body_text:?string}|false $row */
            $row = $db->fetchAssociative(
                "SELECT recipient, template, sent_at, next_attempt_at, body_text FROM outbox_mails WHERE id = $param FOR UPDATE",
                [$key],
            );
            if ($row === false) {
                return null;
            }
            if (TemplateData::secretLink($row['template'])) {
                return 'secret-link';
            }
            if ($row['sent_at'] !== null || $row['next_attempt_at'] !== null) {
                return 'not-failed';
            }
            if (trim($row['body_text'] ?? '') === '') {
                throw new RuntimeException('Failed outbox mail has no body');
            }

            $instant = $this->timestamp($now);
            $db->executeStatement("UPDATE outbox_mails SET attempts = 0, next_attempt_at = ? WHERE id = $param", [$instant, $key]);
            $details = ['mailId' => $id, 'recipient' => $row['recipient']];
            if ($row['template'] !== null && $row['template'] !== '') {
                $details['template'] = $row['template'];
            }
            $db->executeStatement(
                'INSERT INTO audit_events (id, occurred_at, actor_id, action, details) '
                . "VALUES ($param, ?, $param, 'mail-requeued', ?)",
                [$this->idValue(Uuid::v7()->toRfc4122()), $instant, $this->idValue($actorId), json_encode($details, JSON_THROW_ON_ERROR)],
            );

            return $this->find($id);
        });
    }

    /** @return MailLogRow|null */
    private function find(string $id, bool $includeBodies = false): ?array
    {
        $columns = self::METADATA . ($includeBodies ? ', o.body_text, o.body_html' : '');
        /** @var MailLogRow|false $row */
        $row = $this->db->fetchAssociative(
            'SELECT ' . $columns . self::SOURCE . ' WHERE o.id = ' . $this->idParameter(),
            [$this->idValue($id)],
        );

        return $row === false ? null : $this->withTextId($row);
    }

    /**
     * @param MailLogRow $row
     * @return MailLogRow
     */
    private function withTextId(array $row): array
    {
        if (!$this->isPostgres()) {
            $row['id'] = Uuid::fromBinary($row['id'])->toRfc4122();
        }

        return $row;
    }

    private function idParameter(): string
    {
        return $this->isPostgres() ? '?' : 'UNHEX(?)';
    }

    private function idValue(string $id): string
    {
        return $this->isPostgres() ? $id : str_replace('-', '', $id);
    }

    private function timestamp(DateTimeImmutable $instant): string
    {
        return $instant->setTimezone(new DateTimeZone('UTC'))->format($this->isPostgres() ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u');
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
