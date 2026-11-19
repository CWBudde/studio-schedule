<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use Doctrine\DBAL\ArrayParameterType;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;

/** Settles payments under the lesson lock. All IDs use their database representation. */
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
        $paid = $this->redemptions($ids);
        foreach ($ids as $booking) {
            if (isset($paid[$booking])) {
                $this->refund($booking, $paid[$booking]);
            }
        }
    }

    public function restore(string $lesson, bool $onBooking, string $day, DateTimeImmutable $now): void
    {
        $this->settle($this->dueBookings($lesson, $onBooking), [], $day, $now);
    }

    /**
     * @param list<array{id:string,person_id:string}> $charges
     * @param list<string> $refunds Booking IDs.
     */
    public function settle(array $charges, array $refunds, string $day, DateTimeImmutable $now): void
    {
        $ids = array_merge(array_column($charges, 'id'), $refunds);
        if ($ids === []) {
            return;
        }
        // Lock all affected passes together, and return visits before spending them.
        $passes = $this->lockPasses($ids);
        $paid = $this->redemptions($ids);
        foreach ($refunds as $booking) {
            if (isset($paid[$booking])) {
                $this->refund($booking, $paid[$booking]);
            }
        }
        foreach ($charges as $booking) {
            if (!isset($paid[$booking['id']])) {
                $this->redeem($booking['id'], $booking['person_id'], $passes, $day, $now);
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
        $passes = $this->lockPasses($ids);
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
            $this->redeem($redemption['booking_id'], $redemption['person_id'], $passes, $day, $now);
        }
    }

    /** @return list<string> IDs of the owner's passes, held until this transaction ends. */
    public function lockOwnerPasses(string $person): array
    {
        /** @var list<string> $passes */
        $passes = $this->db->fetchFirstColumn(
            'SELECT id FROM passes WHERE person_id = ? ORDER BY id FOR UPDATE',
            [$person],
            [$this->idType()],
        );

        return $passes;
    }

    /**
     * The caller already holds the lesson and the owner's pass locks.
     * @param list<string> $bookingIds
     * @param list<string> $lockedPasses
     */
    public function replaceVoidedRedemptions(
        string $pass,
        string $person,
        array $bookingIds,
        array $lockedPasses,
        string $day,
        DateTimeImmutable $now,
    ): void {
        /** @var list<string> $bookings */
        $bookings = $this->db->fetchFirstColumn(
            'SELECT r.booking_id FROM pass_redemptions r JOIN bookings b ON b.id = r.booking_id '
            . 'WHERE r.pass_id = ? AND b.person_id = ? AND b.id IN (?) ORDER BY b.booked_at, b.id',
            [$pass, $person, $bookingIds],
            [$this->idType(), $this->idType(), $this->idListType()],
        );
        foreach ($bookings as $booking) {
            $this->refund($booking, $pass);
            $this->redeem($booking, $person, $lockedPasses, $day, $now);
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

    /**
     * @param list<string> $bookingIds
     * @return array<array-key,string> Pass ID by booking ID.
     */
    private function redemptions(array $bookingIds): array
    {
        /** @var list<array{pass_id:string,booking_id:string}> $rows */
        $rows = $this->db->fetchAllAssociative(
            'SELECT pass_id, booking_id FROM pass_redemptions WHERE booking_id IN (?)',
            [$bookingIds],
            [$this->idListType()],
        );

        return array_column($rows, 'pass_id', 'booking_id');
    }

    /**
     * @param list<string> $bookingIds
     * @return list<string>
     */
    private function lockPasses(array $bookingIds): array
    {
        /** @var list<string> $locked */
        $locked = $this->db->fetchFirstColumn(
            'SELECT id FROM passes WHERE person_id IN (SELECT person_id FROM bookings WHERE id IN (?)) ORDER BY id FOR UPDATE',
            [$bookingIds],
            [$this->idListType()],
        );

        return $locked;
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

    /** @param list<string> $lockedPasses */
    private function redeem(string $booking, string $person, array $lockedPasses, string $day, DateTimeImmutable $now): void
    {
        if ($lockedPasses === []) {
            return;
        }

        // A concurrent sale can add a pass after the locking read.
        $type = $this->idType();
        /** @var array{id:string,kind:string}|false $pass */
        $pass = $this->db->fetchAssociative(
            "SELECT id, kind FROM passes WHERE id IN (?) AND person_id = ? AND status = 'active' AND valid_from <= ? "
            . "AND (valid_until IS NULL OR valid_until >= ?) AND (kind = 'time' OR visits_remaining > 0) "
            . "ORDER BY CASE WHEN kind = 'time' THEN 0 ELSE 1 END, CASE WHEN valid_until IS NULL THEN 1 ELSE 0 END, valid_until, sold_at, id LIMIT 1",
            [$lockedPasses, $person, $day, $day],
            [$this->idListType(), $type, ParameterType::STRING, ParameterType::STRING],
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
