<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Psr\Clock\ClockInterface;
use StudioSchedule\Store\FamilyStore;

final readonly class LessonSignup
{
    public function __construct(
        private SchedulePersistence $schedule,
        private ParticipantReadPersistence $participants,
        private FamilyStore $family,
        private ClockInterface $clock,
    ) {}

    public function rejectMemberSession(string $id, string $person, SessionPrincipal $principal): void
    {
        $this->schedule->inReadSnapshot(function () use ($id, $person, $principal): void {
            $rules = $this->schedule->rules();
            if ($principal->personId !== $person && !$this->family->isGuardian($principal->personId, $person)) {
                throw new CatalogFailure(404, 'resource not found');
            }

            $lesson = $this->schedule->lesson($id);
            if ($lesson === null) {
                throw new CatalogFailure(404, 'resource not found');
            }
            $now = $this->clock->now();
            $until = ScheduleService::visibleUntil($principal, $rules, $rules->releaseWindow($now)['releasedUntil'], $now);
            if (!ScheduleService::isVisible($lesson, $principal, $until)) {
                throw new CatalogFailure(404, 'resource not found');
            }
            if ($lesson->eventId !== null) {
                throw self::eventSession();
            }
        });
    }

    public function rejectStaffSession(string $id, ?string $person, SessionPrincipal $principal): void
    {
        $this->participants->read(function () use ($id, $person, $principal): void {
            $staffPerson = $principal->role === 'instructor' ? $principal->personId : null;
            $lesson = $this->participants->lesson($id, $staffPerson);
            if ($lesson === null) {
                throw new CatalogFailure(404, 'resource not found');
            }
            if ($person !== null && !$this->family->hasLivePerson($person)) {
                throw CatalogFailure::field('personId', 'person does not exist');
            }
            if ($lesson['eventId'] !== null) {
                throw self::eventSession();
            }
        });
    }

    private static function eventSession(): CatalogFailure
    {
        return new CatalogFailure(409, 'the lesson is a session of an event; register for the event', reason: 'event-session');
    }
}
