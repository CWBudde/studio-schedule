<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Mail\Rendered;

interface LessonNoticePersistence
{
    /**
     * @template T
     * @param Closure():T $write
     * @return T
     */
    public function write(Closure $write): mixed;

    public function lockLesson(string $id): ?PublishedLesson;

    /** @return array<string,true> */
    public function activeStaff(string $person): array;

    /** @return array{personId:string,name:string,email:string}|null */
    public function lockSender(string $account): ?array;

    public function senderNoticeAtOffset(string $account, DateTimeImmutable $since, int $offset): ?DateTimeImmutable;

    public function lessonNoticeCount(string $lesson, DateTimeImmutable $since): int;

    public function enqueue(string $recipient, string $replyTo, Rendered $mail, DateTimeImmutable $now): void;

    public function record(string $lesson, string $sender, DateTimeImmutable $now, int $recipients): void;
}
