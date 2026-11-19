<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use Doctrine\DBAL\ArrayParameterType;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;

/** All methods run under the lesson lock and receive the database representation of its ID. */
final readonly class LessonPaymentStore
{
    public function __construct(private Connection $db) {}

    public function cancel(string $lesson, bool $onBooking): void
    {
        $ids = array_column($this->dueBookings($lesson, $onBooking), 'id');
        if ($ids === []) {
            return;
        }
        $this->lockPasses($ids);
        /** @var list<array{pass_id:string,booking_id:string}> $redemptions */
        $redemptions = $this->db->fetchAllAssociative(
            'SELECT pass_id, booking_id FROM pass_redemptions WHERE booking_id IN (?) ORDER BY booking_id',
            [$ids],
            [$this->idListType()],
        );
        foreach ($redemptions as $redemption) {
            $this->refund($redemption['booking_id'], $redemption['pass_id']);
        }
    }

    public function restore(string $lesson, bool $onBooking, string $day, DateTimeImmutable $now): void
    {
        $bookings = $this->dueBookings($lesson, $onBooking);
        if ($bookings === []) {
            return;
        }
        $this->lockPasses(array_column($bookings, 'id'));
        foreach ($bookings as $booking) {
            if ($this->db->fetchOne('SELECT 1 FROM pass_redemptions WHERE booking_id = ?', [$booking['id']], [$this->idType()]) === false) {
                $this->redeem($booking['id'], $booking['person_id'], $day, $now);
            }
        }
    }

    public function move(string $lesson, string $day, DateTimeImmutable $now): void
    {
        /** @var list<string> $ids */
        $ids = $this->db->fetchFirstColumn(
            'SELECT r.booking_id FROM pass_redemptions r JOIN bookings b ON b.id = r.booking_id WHERE b.lesson_id = ? AND b.person_id IS NOT NULL',
            [$lesson],
            [$this->idType()],
        );
        if ($ids === []) {
            return;
        }
        $this->lockPasses($ids);
        /** @var list<array{pass_id:string,booking_id:string,person_id:string}> $invalid */
        $invalid = $this->db->fetchAllAssociative(
            'SELECT r.pass_id, r.booking_id, b.person_id FROM pass_redemptions r JOIN bookings b ON b.id = r.booking_id '
            . 'JOIN passes p ON p.id = r.pass_id WHERE r.booking_id IN (?) '
            . "AND (p.status <> 'active' OR p.valid_from > ? OR p.valid_until < ?) ORDER BY b.booked_at, b.id",
            [$ids, $day, $day],
            [$this->idListType(), ParameterType::STRING, ParameterType::STRING],
        );
        foreach ($invalid as $redemption) {
            $this->refund($redemption['booking_id'], $redemption['pass_id']);
            $this->redeem($redemption['booking_id'], $redemption['person_id'], $day, $now);
        }
    }

    /** @return list<array{id:string,person_id:string}> */
    private function dueBookings(string $lesson, bool $onBooking): array
    {
        /** @var list<array{id:string,person_id:string}> $bookings */
        $bookings = $this->db->fetchAllAssociative(
            "SELECT b.id, b.person_id FROM bookings b WHERE b.lesson_id = ? AND b.person_id IS NOT NULL AND b.source <> 'member' "
            . 'AND b.trial = FALSE AND ('
            . " (b.status = 'canceled' AND b.late = TRUE)"
            . " OR (b.status <> 'canceled' AND (? = TRUE OR b.status IN ('attended', 'no_show')))) ORDER BY b.booked_at, b.id",
            [$lesson, $onBooking],
            [$this->idType(), ParameterType::BOOLEAN],
        );

        return $bookings;
    }

    /** @param list<string> $bookingIds */
    private function lockPasses(array $bookingIds): void
    {
        $this->db->fetchFirstColumn(
            'SELECT id FROM passes WHERE person_id IN (SELECT person_id FROM bookings WHERE id IN (?)) ORDER BY id FOR UPDATE',
            [$bookingIds],
            [$this->idListType()],
        );
    }

    private function refund(string $booking, string $pass): void
    {
        $type = $this->idType();
        $removed = $this->db->executeStatement(
            'DELETE FROM pass_redemptions WHERE booking_id = ? AND pass_id = ?',
            [$booking, $pass],
            [$type, $type],
        );
        if ($removed === 1) {
            $this->db->executeStatement(
                "UPDATE passes SET visits_remaining = visits_remaining + 1 WHERE id = ? AND kind = 'visits' AND visits_remaining < visits_total",
                [$pass],
                [$type],
            );
        }
    }

    private function redeem(string $booking, string $person, string $day, DateTimeImmutable $now): void
    {
        $type = $this->idType();
        /** @var array{id:string,kind:string}|false $pass */
        $pass = $this->db->fetchAssociative(
            "SELECT id, kind FROM passes WHERE person_id = ? AND status = 'active' AND valid_from <= ? "
            . "AND (valid_until IS NULL OR valid_until >= ?) AND (kind = 'time' OR visits_remaining > 0) "
            . "ORDER BY CASE WHEN kind = 'time' THEN 0 ELSE 1 END, CASE WHEN valid_until IS NULL THEN 1 ELSE 0 END, valid_until, sold_at, id LIMIT 1",
            [$person, $day, $day],
            [$type, ParameterType::STRING, ParameterType::STRING],
        );
        if ($pass === false) {
            return;
        }
        if ($pass['kind'] === 'visits') {
            $this->db->executeStatement('UPDATE passes SET visits_remaining = visits_remaining - 1 WHERE id = ?', [$pass['id']], [$type]);
        }
        $this->db->insert(
            'pass_redemptions',
            ['pass_id' => $pass['id'], 'booking_id' => $booking, 'at' => Database::sqlInstant($this->db, $now)],
            ['pass_id' => $type, 'booking_id' => $type, 'at' => ParameterType::STRING],
        );
    }

    private function idListType(): ArrayParameterType
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform ? ArrayParameterType::STRING : ArrayParameterType::BINARY;
    }

    private function idType(): ParameterType
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform ? ParameterType::STRING : ParameterType::BINARY;
    }
}
