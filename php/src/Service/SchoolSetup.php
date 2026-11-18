<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateInterval;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use Symfony\Component\Uid\Uuid;

final readonly class SchoolSetup
{
    public function __construct(private SetupPersistence $store, private ClockInterface $clock) {}

    public function init(SetupInput $input): SetupResult
    {
        $input = $input->validated();
        $personId = Uuid::v7();
        $accountId = Uuid::v7();
        $token = rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
        $now = $this->clock->now()->setTimezone(new DateTimeZone('UTC'));
        $expiresAt = $now->add(new DateInterval('PT72H'));
        $this->store->initialize($input, $personId, $accountId, hash('sha256', $token), $now, $expiresAt);

        return new SetupResult($accountId->toRfc4122(), $token, $expiresAt);
    }
}
