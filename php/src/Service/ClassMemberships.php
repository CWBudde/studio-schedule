<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use Psr\Clock\ClockInterface;

final readonly class ClassMemberships
{
    /** @param (Closure():void)|null $wakeMail */
    public function __construct(
        private TimetableAdminPersistence $timetable,
        private BookingPersistence $bookings,
        private BookingCancellation $cancellations,
        private ClockInterface $clock,
        private ?Closure $wakeMail = null,
    ) {}

    public function remove(SessionPrincipal $principal, string $classId, string $personId): void
    {
        $offered = $this->bookings->write(function () use ($principal, $classId, $personId): bool {
            $this->timetable->lockTerm($this->timetable->classTerm($classId));
            $class = $this->timetable->lockClassId($classId);
            $this->timetable->removeClassMember($class, $personId);

            $lessons = $this->timetable->futureClassLessons($class, $this->clock->now());

            return $this->cancellations->cancelRemovedMember($principal, $personId, $lessons);
        });
        if ($offered) {
            $this->wakeMail?->__invoke();
        }
    }
}
