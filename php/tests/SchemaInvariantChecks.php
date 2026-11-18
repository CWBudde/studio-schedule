<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\DriverException;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;
use PHPUnit\Framework\Assert;

final readonly class SchemaInvariantChecks
{
    public function __construct(private Connection $connection, private bool $postgres) {}

    public function verify(): void
    {
        $this->connection->beginTransaction();
        try {
            $this->foldedAccountEmails();
            $this->activeCalendarFeeds();
            $lesson = $this->lessonCancellationActor();
            $this->partialBookingAndWaitlistKeys($lesson);
            $this->partialEventRegistrationKey();
            $this->immutableReceipts();
        } finally {
            $this->connection->rollBack();
        }
    }

    private function foldedAccountEmails(): void
    {
        for ($id = 1; $id <= 3; ++$id) {
            $this->connection->executeStatement(
                'INSERT INTO persons (id, given_name, family_name) VALUES (' . $this->id() . ', ?, ?)',
                [self::uuid($id), 'Ada', 'Muster'],
            );
        }
        $insert = 'INSERT INTO accounts (id, person_id, email, role) VALUES ('
            . $this->id() . ', ' . $this->id() . ", ?, 'member')";
        $this->connection->executeStatement($insert, [self::uuid(11), self::uuid(1), 'AÄ@example.invalid']);
        // PostgreSQL under collation "C" folds A but keeps Ä distinct from ä.
        $this->rejectsUniqueKey($insert, [self::uuid(12), self::uuid(2), 'aÄ@example.invalid']);
        $this->connection->executeStatement($insert, [self::uuid(13), self::uuid(3), 'aä@example.invalid']);
    }

    private function activeCalendarFeeds(): void
    {
        $hash = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $insert = 'INSERT INTO calendar_feeds (id, account_id, scope, token_hash, created_at) VALUES ('
            . $this->id() . ', ' . $this->id() . ", 'schedule', $hash, '2026-01-20 09:00:00.123456')";
        $first = [self::uuid(21), self::uuid(11), str_repeat('01', 32)];
        $second = [self::uuid(22), self::uuid(11), str_repeat('02', 32)];
        $this->connection->executeStatement($insert, $first);
        $this->rejectsUniqueKey($insert, $second);

        // A real all-zero UUID is a distinct location from a whole-school feed.
        $this->connection->executeStatement(
            'INSERT INTO locations (id, name) VALUES (' . $this->id() . ', ?)',
            [self::uuid(0), 'Studio'],
        );
        $this->connection->executeStatement(
            'INSERT INTO calendar_feeds (id, account_id, scope, token_hash, created_at, location_id) VALUES ('
            . $this->id() . ', ' . $this->id() . ", 'schedule', $hash, '2026-01-20 09:00:00.123456', " . $this->id() . ')',
            [self::uuid(23), self::uuid(11), str_repeat('03', 32), self::uuid(0)],
        );

        $this->connection->executeStatement(
            "UPDATE calendar_feeds SET revoked_at = '2026-01-20 09:01:00' WHERE id = " . $this->id(),
            [self::uuid(21)],
        );
        $this->connection->executeStatement($insert, $second);
    }

    private function lessonCancellationActor(): string
    {
        $this->connection->executeStatement(
            'INSERT INTO rooms (id, location_id, name, capacity, position) VALUES (' . $this->id() . ', ' . $this->id() . ", 'Hall', 20, 1)",
            [self::uuid(24), self::uuid(0)],
        );
        $insert = 'INSERT INTO lessons (
            id, room_id, starts_at, ends_at, status, visibility, kind, capacity, renter, canceled_by
        ) VALUES (' . $this->id() . ', ' . $this->id() . ", '2026-01-20 13:00:00', '2026-01-20 14:00:00',
            'scheduled', 'hidden', 'rental', 0, 'Test renter', " . $this->id() . ')';
        $actor = self::uuid(11);
        $lesson = self::uuid(25);
        $this->rejectsCancellationActor($insert, [$lesson, self::uuid(24), $actor]);
        $this->connection->executeStatement($insert, [$lesson, self::uuid(24), null]);
        $update = 'UPDATE lessons SET canceled_by = ' . $this->id() . ' WHERE id = ' . $this->id();
        $this->rejectsCancellationActor($update, [$actor, $lesson]);

        $this->connection->executeStatement("UPDATE lessons SET status = 'canceled' WHERE id = " . $this->id(), [$lesson]);
        $this->connection->executeStatement($update, [$actor, $lesson]);
        $this->connection->executeStatement('DELETE FROM accounts WHERE id = ' . $this->id(), [$actor]);
        $cleared = $this->connection->fetchOne(
            'SELECT COUNT(*) FROM lessons WHERE canceled_by IS NULL AND id = ' . $this->id(),
            [$lesson],
        );
        Assert::assertContains($cleared, [1, '1']);

        return $lesson;
    }

    /** @param list<string> $parameters */
    private function rejectsCancellationActor(string $sql, array $parameters): void
    {
        try {
            $this->connection->transactional(static function (Connection $connection) use ($sql, $parameters): void {
                $connection->executeStatement($sql, $parameters);
            });
            Assert::fail('A scheduled lesson accepted a cancellation actor');
        } catch (DriverException $error) {
            $constraint = $this->postgres ? 'lessons_cancellation_when_canceled' : 'Only canceled lessons';
            Assert::assertStringContainsString($constraint, $error->getMessage());
        }
    }

    private function partialBookingAndWaitlistKeys(string $lesson): void
    {
        $person = self::uuid(3);
        $ids = $this->id() . ', ' . $this->id() . ', ' . $this->id();
        $booking = 'INSERT INTO bookings (id, lesson_id, person_id, status, trial, late, released, booked_at, canceled_at, source) '
            . "VALUES ($ids, ?, FALSE, FALSE, FALSE, '2026-01-20 09:00:00', ?, 'self')";
        $this->connection->executeStatement($booking, [self::uuid(41), $lesson, $person, 'canceled', '2026-01-20 10:00:00']);
        $this->connection->executeStatement($booking, [self::uuid(42), $lesson, $person, 'booked', null]);
        $this->rejectsUniqueKey($booking, [self::uuid(43), $lesson, $person, 'attended', null]);
        $this->connection->executeStatement($booking, [self::uuid(44), $lesson, $person, 'canceled', '2026-01-20 10:00:00']);

        $waitlist = 'INSERT INTO waitlist_entries (id, lesson_id, person_id, status, joined_at, stay_on_list) '
            . "VALUES ($ids, ?, '2026-01-20 09:00:00', FALSE)";
        $this->connection->executeStatement($waitlist, [self::uuid(45), $lesson, $person, 'withdrawn']);
        $this->connection->executeStatement($waitlist, [self::uuid(46), $lesson, $person, 'waiting']);
        $this->rejectsUniqueKey($waitlist, [self::uuid(47), $lesson, $person, 'waiting']);

        $hash = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $invite = 'INSERT INTO waitlist_entries (id, lesson_id, person_id, status, joined_at, stay_on_list,'
            . ' invite_token_hash, invited_at, expires_at) '
            . "VALUES ($ids, 'invited', '2026-01-20 09:00:00', FALSE, $hash, '2026-01-20 10:00:00', '2026-01-20 11:00:00')";
        $this->rejectsUniqueKey($invite, [self::uuid(48), $lesson, $person, str_repeat('04', 32)]);
    }

    private function partialEventRegistrationKey(): void
    {
        $courseType = self::uuid(51);
        $event = self::uuid(52);
        $person = self::uuid(3);
        $this->connection->executeStatement(
            'INSERT INTO course_types (id, name) VALUES (' . $this->id() . ", 'Dance')",
            [$courseType],
        );
        $this->connection->executeStatement(
            'INSERT INTO events (id, slug, title, course_type_id, capacity, price, vat_rate_bp, status, created_at, updated_at)'
            . ' VALUES (' . $this->id() . ", 'workshop', 'Workshop', " . $this->id()
            . ", 30, 0, 0, 'published', '2026-01-20 09:00:00', '2026-01-20 09:00:00')",
            [$event, $courseType],
        );
        $ids = $this->id() . ', ' . $this->id() . ', ' . $this->id();
        $registration = 'INSERT INTO event_registrations (id, event_id, person_id, status, payment, price, vat_rate_bp,'
            . ' created_at, updated_at, canceled_at) '
            . "VALUES ($ids, ?, 'unpaid', 0, 0, '2026-01-20 09:00:00', '2026-01-20 09:00:00', ?)";
        $this->connection->executeStatement($registration, [self::uuid(53), $event, $person, 'canceled', '2026-01-20 10:00:00']);
        $this->connection->executeStatement($registration, [self::uuid(54), $event, $person, 'registered', null]);
        $this->rejectsUniqueKey($registration, [self::uuid(55), $event, $person, 'waiting', null]);
    }

    private function immutableReceipts(): void
    {
        $this->connection->executeStatement(
            'INSERT INTO receipts (
                id, number, year, sequence, kind, issued_at, issue_date, locale,
                issuer_name, issuer_address, issuer_tax_id, tax_mode, recipient_name,
                payment_method, currency, net, vat, gross
            ) VALUES (' . $this->id() . ", 'TS-2026-1', 2026, 1, 'sale',
                '2026-01-20 09:00:00', '2026-01-20', 'de', 'School', 'Test address',
                'Test tax ID', 'standard', 'Recipient', 'cash', 'EUR', 100, 19, 119)",
            [self::uuid(31)],
        );
        foreach (['UPDATE receipts SET gross = 120', 'DELETE FROM receipts'] as $sql) {
            try {
                $this->connection->transactional(function (Connection $connection) use ($sql): void {
                    $connection->executeStatement($sql . ' WHERE id = ' . $this->id(), [self::uuid(31)]);
                });
                Assert::fail('The issued receipt could be modified: ' . $sql);
            } catch (DriverException $error) {
                Assert::assertStringContainsString('immutable', strtolower($error->getMessage()));
            }
        }
    }

    /** @param list<string|null> $parameters */
    private function rejectsUniqueKey(string $sql, array $parameters): void
    {
        try {
            $this->connection->transactional(static function (Connection $connection) use ($sql, $parameters): void {
                $connection->executeStatement($sql, $parameters);
            });
            Assert::fail('A duplicate was accepted: ' . $sql);
        } catch (UniqueConstraintViolationException $error) {
            Assert::assertNotNull($error->getSQLState());
        }
    }

    private function id(): string
    {
        return $this->postgres ? 'CAST(? AS uuid)' : "UNHEX(REPLACE(?, '-', ''))";
    }

    private static function uuid(int $suffix): string
    {
        return sprintf('00000000-0000-0000-0000-%012x', $suffix);
    }
}
