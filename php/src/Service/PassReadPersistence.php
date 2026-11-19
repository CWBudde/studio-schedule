<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use StudioSchedule\Domain\PublishedLesson;

/**
 * @phpstan-type PersonRef array{id:string,name:string,anonymized?:true}
 * @phpstan-type PassDocument array{
 *     id:string, passType:array{id:string,name:string}, owner:PersonRef, kind:string, visitsTotal:int, visitsRemaining:int,
 *     validFrom:string, validUntil?:string, status:string, origin:string, soldAt:string, payer?:PersonRef,
 *     pricePaid:int, voidedAt?:string, receiptId?:string, note?:string
 * }
 * @phpstan-type Redemption array{
 *     passId:string, bookingId:string, lessonId:string, bookingStatus:string, late:bool, redeemedAt:string, useCount:int
 * }
 */
interface PassReadPersistence
{
    /**
     * @template T
     * @param Closure():T $read
     * @return T
     */
    public function read(Closure $read): mixed;

    /** @return array{timezone:string,redemption_mode:string}|null */
    public function school(): ?array;

    public function personExists(string $id): bool;

    /** @return list<PassDocument> */
    public function passes(string $person, bool $family): array;

    /** @return PassDocument|null */
    public function byId(string $id): ?array;

    /** @return list<Redemption> */
    public function redemptionsForPass(string $id): array;

    /** @return list<Redemption> */
    public function redemptions(string $person, bool $family): array;

    /**
     * @param list<string> $ids
     * @return list<PublishedLesson>
     */
    public function lessons(array $ids): array;
}
