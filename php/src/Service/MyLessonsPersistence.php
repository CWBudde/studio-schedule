<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;

/**
 * @phpstan-import-type PaymentPass from ParticipantReadPersistence
 * @phpstan-type FamilyBooking array{id:string,lessonId:string,personId:string,status:string,source:string,
 *     trial:bool,late:bool,bookedAt:string,canceledAt:?string,pass:?PaymentPass}
 */
interface MyLessonsPersistence
{
    public function redemptionOnBooking(): bool;

    /**
     * @param list<string> $people
     * @return list<FamilyBooking>
     */
    public function upcoming(array $people, DateTimeImmutable $now): array;

    /**
     * @param list<string> $people
     * @return list<FamilyBooking>
     */
    public function history(array $people, DateTimeImmutable $now, DateTimeImmutable $from, DateTimeImmutable $before): array;

    /** @param list<string> $people */
    public function hasHistoryBefore(array $people, DateTimeImmutable $now, DateTimeImmutable $before): bool;

    /**
     * @param list<string> $people
     * @return list<string>
     */
    public function waitingLessonIds(array $people, DateTimeImmutable $now): array;
}
