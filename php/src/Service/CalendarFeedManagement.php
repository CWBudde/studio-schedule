<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\CalendarFeed;
use StudioSchedule\Domain\CalendarFeedProblem;
use StudioSchedule\Domain\CalendarFeedScope;
use StudioSchedule\Store\CalendarFeedStore;
use Symfony\Component\Uid\Uuid;

final readonly class CalendarFeedManagement
{
    public function __construct(private CalendarFeedStore $store, private ClockInterface $clock) {}

    /** @return list<CalendarFeed> */
    public function list(SessionPrincipal $principal): array
    {
        return $this->store->list($principal->accountId);
    }

    /** @return array{feed:CalendarFeed,token:string}|CalendarFeedProblem */
    public function create(SessionPrincipal $principal, CalendarFeedScope $scope, ?string $locationId): array|CalendarFeedProblem
    {
        if ($principal->impersonatorId !== null
            || ($scope === CalendarFeedScope::Teaching && !in_array($principal->role, ['instructor', 'office', 'admin'], true))) {
            return CalendarFeedProblem::Forbidden;
        }

        $token = rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
        $feed = new CalendarFeed(Uuid::v7()->toRfc4122(), $scope, $this->clock->now(), $locationId);
        $created = $this->store->replace($principal, $feed, hash('sha256', $token));

        return $created instanceof CalendarFeed ? ['feed' => $created, 'token' => $token] : $created;
    }

    public function revoke(SessionPrincipal $principal, string $feedId): bool
    {
        return $this->store->revoke($principal->accountId, $feedId, $this->clock->now());
    }
}
