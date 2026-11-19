<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Domain\Instant;

final readonly class WaitlistInvites
{
    public function __construct(private WaitlistInviteReader $store, private ClockInterface $clock) {}

    /** @return array<string,mixed>|null */
    public function view(#[SensitiveParameter] string $token): ?array
    {
        $entry = $this->store->lookup(hash('sha256', $token));
        if ($entry === null) {
            return null;
        }

        $now = $this->clock->now();
        $expires = $entry['expiresAt'];
        $lesson = $entry['lesson'];
        $cutoff = $lesson->startsAt->modify('-' . $entry['rules']->bookingCutoffMicros . ' microseconds');
        $closed = match (true) {
            $lesson->kind === 'rental' => 'rental',
            $lesson->status === 'canceled' => 'canceled',
            $now >= $lesson->startsAt => 'started',
            $now >= $cutoff => 'cutoff',
            $lesson->visibility === 'hidden' => 'release',
            default => null,
        };
        $state = $entry['status'] === 'invited' && ($expires === null || $now >= $expires)
            ? 'expired' : $entry['status'];

        $summary = ScheduleService::lessonSummary($lesson);
        unset($summary['eventSlug']);

        return [
            'state' => $state,
            'givenName' => $entry['givenName'],
            'lesson' => $summary,
        ] + array_filter([
            'expiresAt' => $expires === null ? null : Instant::format($expires),
            'position' => $entry['position'] === 0 ? null : $entry['position'],
            'closed' => $closed,
        ], static fn(mixed $value): bool => $value !== null);
    }
}
