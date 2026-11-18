<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateInterval;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;

final readonly class RetentionStore
{
    private const string ANONYMOUS_GUEST = 'Guest';

    public function __construct(private Connection $database) {}

    /**
     * Apply ADR 0016 in one transaction, including the later event and push retention rules.
     *
     * @return array{guests:int,eventGuests:int,auditEvents:int,importRuns:int,pushMessages:int,jobRuns:int,pushSubscriptions:int}
     */
    public function purge(DateTimeImmutable $now): array
    {
        $now = $now->setTimezone(new DateTimeZone('UTC'));

        return $this->database->transactional(function (Connection $database) use ($now): array {
            $guest = self::ANONYMOUS_GUEST;
            // MySQL and MariaDB can ignore trailing spaces in text comparisons.
            $guestBytes = strlen($guest);
            $guestCutoff = $this->cutoff($now, 'P12M');
            $guests = (int) $database->executeStatement(
                'UPDATE bookings SET guest_name = ?, guest_email = NULL, trial_note = NULL '
                . 'WHERE person_id IS NULL AND (guest_name <> ? OR OCTET_LENGTH(guest_name) <> ? OR guest_email IS NOT NULL OR trial_note IS NOT NULL) '
                . 'AND EXISTS (SELECT 1 FROM lessons WHERE lessons.id = bookings.lesson_id AND lessons.ends_at < ?)',
                [$guest, $guest, $guestBytes, $guestCutoff],
            );
            $eventGuests = (int) $database->executeStatement(
                'UPDATE event_registrations SET guest_name = ?, guest_email = NULL '
                . 'WHERE person_id IS NULL AND (guest_name <> ? OR OCTET_LENGTH(guest_name) <> ? OR guest_email IS NOT NULL) '
                . 'AND event_id IN (SELECT event_id FROM lessons WHERE event_id IS NOT NULL GROUP BY event_id HAVING MAX(ends_at) < ?)',
                [$guest, $guest, $guestBytes, $guestCutoff],
            );
            $auditEvents = (int) $database->executeStatement('DELETE FROM audit_events WHERE occurred_at < ?', [$this->cutoff($now, 'P3Y')]);
            $importRuns = (int) $database->executeStatement('DELETE FROM import_runs WHERE started_at < ?', [$this->cutoff($now, 'P1Y')]);
            $pushMessages = (int) $database->executeStatement('DELETE FROM push_outbox WHERE created_at < ?', [$this->cutoff($now, 'P7D')]);
            $jobRuns = (int) $database->executeStatement('DELETE FROM job_runs WHERE started_at < ?', [$this->cutoff($now, 'P30D')]);
            $pushSubscriptions = (int) $database->executeStatement('DELETE FROM push_subscriptions WHERE last_used_at < ?', [$this->cutoff($now, 'P6M')]);

            return [
                'guests' => $guests,
                'eventGuests' => $eventGuests,
                'auditEvents' => $auditEvents,
                'importRuns' => $importRuns,
                'pushMessages' => $pushMessages,
                'jobRuns' => $jobRuns,
                'pushSubscriptions' => $pushSubscriptions,
            ];
        });
    }

    private function cutoff(DateTimeImmutable $utc, string $period): string
    {
        $format = $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u';

        return $utc->sub(new DateInterval($period))->format($format);
    }
}
