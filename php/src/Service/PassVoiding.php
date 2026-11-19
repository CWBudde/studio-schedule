<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeZone;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Date;
use StudioSchedule\Store\PassLedgerStore;

final readonly class PassVoiding
{
    public function __construct(
        private EventPersistence $transactions,
        private PassLedgerStore $ledger,
        private PassRead $passes,
        private PassReadPersistence $reads,
        private ClockInterface $clock,
    ) {}

    /** @return array<string,mixed> */
    public function void(string $id): array
    {
        return $this->transactions->write(function () use ($id): array {
            $school = $this->reads->school() ?? throw new CatalogFailure(404, 'resource not found');
            $zone = new DateTimeZone($school['timezone']);
            $now = $this->clock->now();
            $this->ledger->void($id, $now, $zone);

            return $this->passes->officePass($id, (string) Date::fromInstant($now, $zone));
        });
    }
}
