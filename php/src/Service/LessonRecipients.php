<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use StudioSchedule\Domain\PublishedLesson;

/** @phpstan-import-type Message from ScheduleRecipients */
final readonly class LessonRecipients
{
    public function __construct(
        private LessonWritePersistence $lessons,
        private EventRegistrationPersistence $registrations,
        private ScheduleRecipients $recipients,
    ) {}

    /** @return list<Message> */
    public function forLesson(PublishedLesson $lesson): array
    {
        $parties = $this->lessons->bookedRecipients($lesson->id);
        if ($lesson->eventId !== null) {
            foreach ($this->registrations->forEvent($lesson->eventId) as $registration) {
                if ($registration->status === 'registered') {
                    $parties[] = ['personId' => $registration->person['id'] ?? null, 'guest' => $registration->guest];
                }
            }
        }

        return $this->recipients->group($parties);
    }
}
