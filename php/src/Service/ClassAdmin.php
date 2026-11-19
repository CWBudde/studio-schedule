<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\ScheduleRules;

/**
 * @phpstan-import-type CatalogDocument from CatalogAdminPersistence
 * @phpstan-import-type TermRow from LessonGenerationPersistence
 * @phpstan-import-type ClassRow from LessonGenerationPersistence
 */
final readonly class ClassAdmin
{
    /** @param (Closure():void)|null $wakeMail */
    public function __construct(
        private TimetableAdminPersistence $store,
        private LessonGenerator $generator,
        private ClockInterface $clock,
        private EventPersistence $transaction,
        private ClassLessonSync $sync,
        private ?Closure $wakeMail = null,
    ) {}

    /** @return CatalogDocument */
    public function create(string $termId, ClassData $data): array
    {
        return $this->store->write(function () use ($termId, $data): array {
            $rules = $this->rulesIfConfigured();
            $term = $this->store->lockTerm($termId);
            $data->validate();
            $class = $this->store->saveClass($term['id'], $data);
            if ($rules !== null) {
                $this->generate($class, $term, $rules, $this->clock->now());
            }

            return $this->store->classDocument($class['id']);
        });
    }

    /** @return CatalogDocument */
    public function update(string $id, ClassData $data): array
    {
        [$result, $queued] = $this->transaction->write(function () use ($id, $data): array {
            $termId = $this->store->classTerm($id);
            $rules = $this->rulesIfConfigured();
            $term = $this->store->lockTerm($termId);
            $old = $this->store->lockClass($id);
            $data->validate();
            $class = $this->store->saveClass($term['id'], $data, $old['id']);
            if ($data->capacity < (int) $old['capacity']) {
                $members = count($this->store->lockMembers($old['id']));
                if ($members > $data->capacity) {
                    throw CatalogFailure::field('capacity', "the class has $members members, more than the capacity");
                }
            }
            $queued = false;
            if ($rules !== null) {
                $queued = $this->sync->reconcile($old, $class, $term, $rules);
                $this->generate($class, $term, $rules, $this->clock->now());
            }

            return [$this->store->classDocument($class['id']), $queued];
        });
        if ($queued) {
            $this->wakeMail?->__invoke();
        }

        return $result;
    }

    public function delete(string $id): void
    {
        $this->store->write(function () use ($id): void {
            $class = $this->store->lockClassId($id);
            if ($this->store->lockMembers($class) !== []) {
                throw self::inUse();
            }
            $lessons = $this->store->lockClassLessons($class, $this->clock->now());
            foreach ($lessons as $lesson) {
                if ($lesson['started'] || $this->store->hasOwnBookings($lesson['id'])) {
                    throw self::inUse();
                }
            }
            try {
                foreach ($lessons as $lesson) {
                    $this->store->deleteClassLesson($lesson['id']);
                }
                $this->store->deleteClass($class);
            } catch (CatalogFailure $error) {
                throw $error->status === 409 ? self::inUse() : $error;
            }
        });
    }

    private function rulesIfConfigured(): ?ScheduleRules
    {
        try {
            // Match the settings and term lock order used by lesson generation.
            return $this->store->lockRules();
        } catch (SettingsNotFound) {
            return null;
        }
    }

    /**
     * @param ClassRow $class
     * @param TermRow $term
     */
    private function generate(array $class, array $term, ScheduleRules $rules, DateTimeImmutable $now): void
    {
        $today = Date::fromInstant($now, $rules->timezone);
        $until = $rules->generationEnd($today, $this->store->longestVisibleDays());
        $this->generator->fillLockedClass($class, $term, $today, $until, $rules->timezone, $now);
    }

    private static function inUse(): CatalogFailure
    {
        return new CatalogFailure(409, 'the record is still in use', reason: 'in-use');
    }
}
