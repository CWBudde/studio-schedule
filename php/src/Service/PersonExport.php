<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Store\FamilyStore;
use StudioSchedule\Store\PersonExportStore;

final readonly class PersonExport
{
    public function __construct(
        private PersonExportStore $store,
        private FamilyStore $family,
        private ClockInterface $clock,
    ) {}

    /**
     * The family person is null for an authorized office request.
     *
     * @return array<string,mixed>|null
     */
    public function document(string $id, ?string $familyPerson): ?array
    {
        return $this->family->inReadSnapshot(function () use ($id, $familyPerson): ?array {
            $includeAccount = $familyPerson === null || $familyPerson === $id;
            if (!$includeAccount && !$this->family->isGuardian($familyPerson, $id)) {
                return null;
            }
            $person = $this->store->person($id);
            if ($person === null) {
                return null;
            }
            $at = Instant::format($this->clock->now());

            return [
                'format' => 'studio-schedule.person-export', 'version' => 1, 'exportedAt' => $at,
                'person' => $person,
            ] + $this->store->familyLinks($id)
                + ($includeAccount ? $this->store->accountData($id) : [])
                + ['calendarFeeds' => [], 'pushDevices' => [], 'dismissedAnnouncements' => []]
                + [
                    'classMemberships' => $this->store->classMemberships($id),
                    'bookings' => $this->store->bookings($id),
                    'waitlistEntries' => $this->store->waitlistEntries($id),
                    'eventRegistrations' => $this->store->eventRegistrations($id),
                    'passes' => $this->store->passes($id),
                    'receipts' => $this->store->receipts($id),
                ];
        });
    }
}
