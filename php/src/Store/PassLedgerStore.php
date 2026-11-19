<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Date;
use StudioSchedule\Service\CatalogFailure;
use Symfony\Component\Uid\Uuid;

/** Pass mutations inside a write transaction. Lesson locks come before pass locks. */
final readonly class PassLedgerStore
{
    public function __construct(private Connection $db) {}

    public function void(string $id, DateTimeImmutable $now, DateTimeZone $zone): void
    {
        $key = $this->key($id);
        $observed = $this->pass('id', $key) ?? throw self::notFound();
        self::requireActive($observed['status']);
        $ahead = $this->lockFutureLessons($key, $now);
        $payments = new LessonPaymentStore($this->db);
        $owner = $observed['person_id'];
        $locked = $payments->lockOwnerPasses($owner);
        if (!in_array($key, $locked, true)) {
            throw self::notFound();
        }
        $pass = $this->pass('id', $key) ?? throw self::notFound();
        self::requireActive($pass['status']);
        $this->markVoided($pass, $now);
        foreach ($ahead as $lesson) {
            $payments->replaceVoidedRedemptions(
                $key,
                $owner,
                $lesson['bookings'],
                $locked,
                (string) Date::fromInstant($lesson['start'], $zone),
                $now,
            );
        }
    }

    public function voidUnusedReceiptPass(string $receipt, ClockInterface $clock): void
    {
        $observed = $this->pass('receipt_id', $this->key($receipt));
        if ($observed === null) {
            return;
        }
        $id = $observed['id'];
        $locked = (new LessonPaymentStore($this->db))->lockOwnerPasses($observed['person_id']);
        if (!in_array($id, $locked, true)) {
            throw new CatalogFailure(409, 'the pass changed its owner meanwhile');
        }
        $pass = $this->pass('id', $id) ?? throw self::notFound();
        if ($this->db->fetchOne(
            'SELECT 1 FROM pass_redemptions WHERE pass_id = ? LIMIT 1',
            [$id],
            [$this->idType()],
        ) !== false) {
            throw new CatalogFailure(409, 'the pass paid for visits; void the pass instead', reason: 'pass-used');
        }
        if ($pass['status'] !== 'voided') {
            $this->markVoided($pass, $clock->now());
        }
    }

    /** @return list<array{start:DateTimeImmutable,bookings:list<string>}> */
    private function lockFutureLessons(string $pass, DateTimeImmutable $now): array
    {
        /** @var list<array{id:string,lesson_id:string}> $rows */
        $rows = $this->db->fetchAllAssociative(
            'SELECT b.id, b.lesson_id FROM pass_redemptions r JOIN bookings b ON b.id = r.booking_id '
            . 'JOIN lessons l ON l.id = b.lesson_id WHERE r.pass_id = ? AND l.starts_at > ? ORDER BY l.id, b.booked_at, b.id',
            [$pass, Database::sqlInstant($this->db, $now)],
            [$this->idType(), ParameterType::STRING],
        );
        $bookings = [];
        foreach ($rows as $row) {
            $bookings[$row['lesson_id']][] = $row['id'];
        }

        $ahead = [];
        foreach ($bookings as $id => $bookingIds) {
            /** @var string|false $start */
            $start = $this->db->fetchOne('SELECT starts_at FROM lessons WHERE id = ? FOR UPDATE', [$id], [$this->idType()]);
            if ($start === false) {
                throw self::notFound();
            }
            $startsAt = Database::instant($start);
            if ($startsAt > $now) {
                $ahead[] = ['start' => $startsAt, 'bookings' => $bookingIds];
            }
        }

        return $ahead;
    }

    /**
     * @param 'id'|'receipt_id' $field
     * @return array{id:string,person_id:string,status:string,sold_at:string}|null
     */
    private function pass(string $field, string $key): ?array
    {
        /** @var array{id:string,person_id:string,status:string,sold_at:string}|false $row */
        $row = $this->db->fetchAssociative("SELECT id, person_id, status, sold_at FROM passes WHERE $field = ?", [$key], [$this->idType()]);

        return $row === false ? null : $row;
    }

    /** @param array{id:string,sold_at:string} $pass */
    private function markVoided(array $pass, DateTimeImmutable $now): void
    {
        if ($now < Database::instant($pass['sold_at'])) {
            throw new CatalogFailure(422, 'a pass cannot be voided before it was sold');
        }
        $this->db->executeStatement(
            "UPDATE passes SET status = 'voided', voided_at = ? WHERE id = ?",
            [Database::sqlInstant($this->db, $now), $pass['id']],
            [ParameterType::STRING, $this->idType()],
        );
    }

    private static function requireActive(string $status): void
    {
        if ($status === 'voided') {
            throw new CatalogFailure(409, 'the pass is voided already', reason: 'already-voided');
        }
    }

    private static function notFound(): CatalogFailure
    {
        return new CatalogFailure(404, 'resource not found');
    }

    private function key(string $id): string
    {
        return $this->idType() === ParameterType::STRING ? $id : Uuid::fromString($id)->toBinary();
    }

    private function idType(): ParameterType
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform ? ParameterType::STRING : ParameterType::BINARY;
    }
}
