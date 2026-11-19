<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\AnnouncementAudience;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Mail\Renderer;
use StudioSchedule\Store\AnnouncementRecipients;
use StudioSchedule\Store\AnnouncementStore;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\OutboxStore;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type AnnouncementRow from AnnouncementStore
 * @phpstan-import-type OfficeRow from AnnouncementStore
 */
final readonly class Announcements
{
    private const int MAILS_PER_DAY = 3;
    private const int RECIPIENTS_PER_RUN = 100;

    /** @param (Closure():void)|null $wake */
    public function __construct(
        private AnnouncementStore $store,
        private AnnouncementRecipients $recipients,
        private OutboxStore $outbox,
        private Renderer $renderer,
        private ClockInterface $clock,
        private ?Closure $wake = null,
    ) {}

    /** @return list<array<string,mixed>> */
    public function mine(SessionPrincipal $principal): array
    {
        return $this->store->read(fn(): array => $this->shown($principal, $this->clock->now()));
    }

    public function dismiss(SessionPrincipal $principal, string $id): void
    {
        $this->store->write(function () use ($principal, $id): void {
            $now = $this->clock->now();
            if ($this->store->lock($id) === null || $this->shown($principal, $now, $id) === []) {
                throw new AnnouncementProblem(404, 'resource not found');
            }
            if ($principal->impersonatorId === null) {
                $this->store->dismiss($id, $principal->accountId, $now);
            }
        });
    }

    /** @return list<array<string,mixed>> */
    private function shown(SessionPrincipal $principal, DateTimeImmutable $now, ?string $id = null): array
    {
        $today = $now->setTimezone(new DateTimeZone($this->store->settings()['timezone']))->format('Y-m-d');

        return $this->store->shown($principal, $now, $today, $id);
    }

    /** @return list<array<string,mixed>> */
    public function listAll(): array
    {
        return $this->store->read(fn(): array => array_map($this->officeView(...), $this->store->officeList()));
    }

    /** @return array<string,mixed> */
    public function save(SessionPrincipal $principal, AnnouncementInput $input, ?string $id = null): array
    {
        [$row, $due] = $this->store->write(function () use ($principal, $input, $id): array {
            $this->store->lockMailingLimit();
            $now = $this->clock->now();
            $row = $id === null ? null : ($this->store->lock($id) ?? throw new AnnouncementProblem(404, 'resource not found'));
            if ($row !== null && $row['publish_at'] !== null && self::expired($row, $now)) {
                throw new AnnouncementProblem(409, 'the announcement has expired', reason: 'expired');
            }
            if ($input->draft && $row !== null && $row['publish_at'] !== null) {
                throw AnnouncementProblem::field('draft', 'a published announcement cannot become a draft');
            }
            if ($input->draft && $input->publishAt !== null) {
                throw AnnouncementProblem::field('publishAt', 'a draft has no publish time');
            }

            $publish = $input->draft ? null : ($input->publishAt ?? $now);
            if ($input->expiresAt !== null && ($input->expiresAt <= $now || ($publish !== null && $input->expiresAt <= $publish))) {
                throw AnnouncementProblem::field('expiresAt', 'must expire in the future and after publication');
            }
            $this->validateReference($input->audience);

            $writeId = $id ?? Uuid::v7()->toRfc4122();
            $started = $row !== null && ($row['emailed_at'] !== null || $row['mail_cursor'] !== null);
            $email = $started || $input->email;
            if (!$started && $email && $publish !== null) {
                $zone = new DateTimeZone($this->store->settings()['timezone']);
                $day = max($publish, $now)->setTimezone($zone)->setTime(0, 0);
                if ($this->store->mailLimitUsed($day, $day->modify('+1 day'), $writeId) >= self::MAILS_PER_DAY) {
                    throw new AnnouncementProblem(409, $day->format('Y-m-d') . ' has 3 emailed announcements already', reason: 'mail-limit');
                }
            }
            $this->store->save($writeId, $principal->impersonatorId ?? $principal->accountId, $input, $publish, $email, $now, $id === null);
            $saved = $this->store->officeList($writeId)[0] ?? throw new \LogicException('Announcement is missing after save');

            return [$saved, $email && $saved['emailed_at'] === null && $publish !== null && $publish <= $now];
        });
        if ($due && $this->wake !== null) {
            ($this->wake)();
        }

        return $this->officeView($row);
    }

    public function delete(string $id): void
    {
        $this->store->write(function () use ($id): void {
            $this->store->lockMailingLimit();
            $row = $this->store->lock($id) ?? throw new AnnouncementProblem(404, 'resource not found');
            if ($row['emailed_at'] === null && (int) $row['email_recipients'] > 0) {
                $this->store->recordMailing($id, $this->store->decode($row['author_id']), $this->clock->now(), (int) $row['email_recipients']);
            }
            $this->store->delete($id);
        });
    }

    public function countRecipients(AnnouncementAudience $audience): int
    {
        return $this->store->read(function () use ($audience): int {
            $this->validateReference($audience);

            return count($this->recipients->find($audience, $this->clock->now(), $this->store->settings()['timezone']));
        });
    }

    public function queueDue(): int
    {
        return $this->store->write(function (): int {
            $this->store->lockMailingLimit();
            $now = $this->clock->now();
            $due = $this->store->due($now);
            if ($due === []) {
                return 0;
            }

            $settings = $this->store->settings();
            $queued = 0;
            foreach ($due as $row) {
                $remaining = self::RECIPIENTS_PER_RUN - $queued;
                if ($remaining === 0) {
                    break;
                }
                $recipients = self::expired($row, $now) ? [] : $this->recipients->find($this->audienceOf($row), $now, $settings['timezone']);
                $cursor = $row['mail_cursor'] ?? '';
                $batch = 0;
                $more = false;
                foreach ($recipients as $key => $recipient) {
                    if (strcmp($key, $cursor) <= 0) {
                        continue;
                    }
                    if ($batch === $remaining) {
                        $more = true;
                        break;
                    }
                    $mail = $this->renderer->render(
                        $this->renderer->matchLocale($recipient['locale']) ?? $settings['locale'],
                        'announcement',
                        ['SchoolName' => $settings['school_name'], 'GivenName' => $recipient['givenName'],
                            'Participants' => $recipient['participants'], 'Title' => $row['title'], 'Message' => $row['body']],
                    );
                    $this->outbox->enqueue($recipient['email'], 'announcement', $mail, $now);
                    $cursor = $key;
                    ++$batch;
                }

                $count = (int) $row['email_recipients'] + $batch;
                $id = $this->store->decode($row['id']);
                $this->store->mailProgress($id, $count, $more ? $cursor : null, $more ? null : $now);
                if (!$more && $count > 0) {
                    $this->store->recordMailing($id, $this->store->decode($row['author_id']), $now, $count);
                }
                $queued += $batch;
            }

            return $queued;
        });
    }

    private function validateReference(AnnouncementAudience $audience): void
    {
        if ($audience->reference === null) {
            return;
        }
        $kind = $this->store->referenceKind($audience);
        $field = AnnouncementAudience::ID_FIELDS[$audience->kind];
        if ($kind === false || $kind === 'rental') {
            throw AnnouncementProblem::field($field, $kind === 'rental' ? 'a room rental has no participants' : 'the audience does not exist');
        }
    }

    /** @param AnnouncementRow $row */
    private function audienceOf(array $row): AnnouncementAudience
    {
        $column = AnnouncementStore::REFERENCE_COLUMNS[$row['audience']] ?? null;
        $id = $column === null ? null : $row[$column];

        return new AnnouncementAudience($row['audience'], $row['role'], $id === null ? null : $this->store->decode($id));
    }

    /** @param AnnouncementRow $row */
    private static function expired(array $row, DateTimeImmutable $now): bool
    {
        return $row['expires_at'] !== null && Database::instant($row['expires_at']) <= $now;
    }

    /**
     * @param OfficeRow $row
     * @return array<string,mixed>
     */
    private function officeView(array $row): array
    {
        $view = [
            'id' => $this->store->decode($row['id']), 'audience' => $row['audience'], 'title' => $row['title'],
            'body' => $row['body'], 'author' => $row['author'], 'emailRecipients' => (int) $row['email_recipients'],
            'createdAt' => Instant::format(Database::instant($row['created_at'])),
            'updatedAt' => Instant::format(Database::instant($row['updated_at'])),
        ];
        if (AnnouncementStore::boolean($row['email'])) {
            $view['email'] = true;
        }
        if ($row['publish_at'] === null) {
            $view['draft'] = true;
        }
        foreach (['publishAt' => 'publish_at', 'expiresAt' => 'expires_at', 'emailedAt' => 'emailed_at'] as $field => $column) {
            if ($row[$column] !== null) {
                $view[$field] = Instant::format(Database::instant($row[$column]));
            }
        }
        $audience = $this->audienceOf($row);
        if ($audience->role !== null) {
            $view['role'] = $audience->role;
        }
        if ($audience->reference !== null) {
            $view[AnnouncementAudience::ID_FIELDS[$audience->kind]] = $audience->reference;
        }
        if ($row['event_title'] !== null) {
            $view['eventTitle'] = $row['event_title'];
        }
        if ($row['description_course_id'] !== null && $row['description_course_name'] !== null) {
            $course = ['id' => $this->store->decode($row['description_course_id']), 'name' => $row['description_course_name']];
            if ($audience->kind === 'course-type') {
                $view['courseType'] = $course;
            }
            if ($audience->kind === 'class' && $row['start_time'] !== null && $row['term_id'] !== null) {
                $view['class'] = ['id' => $audience->reference, 'courseType' => $course, 'weekday' => (int) $row['weekday'],
                    'startTime' => substr($row['start_time'], 0, 5), 'termId' => $this->store->decode($row['term_id']), 'termName' => $row['term_name']];
            }
            if ($audience->kind === 'lesson' && $row['starts_at'] !== null && $row['ends_at'] !== null) {
                $view['lesson'] = ['id' => $audience->reference, 'courseType' => $course,
                    'startsAt' => Instant::format(Database::instant($row['starts_at'])), 'endsAt' => Instant::format(Database::instant($row['ends_at']))];
            }
        }

        return $view;
    }
}
