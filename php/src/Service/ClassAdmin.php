<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Psr\Clock\ClockInterface;
use StudioSchedule\Domain\Date;

/** @phpstan-import-type CatalogDocument from CatalogAdminPersistence */
final readonly class ClassAdmin
{
    public function __construct(
        private TimetableAdminPersistence $store,
        private LessonGenerator $generator,
        private ClockInterface $clock,
    ) {}

    /** @return CatalogDocument */
    public function create(string $termId, ClassData $data): array
    {
        return $this->store->write(function () use ($termId, $data): array {
            try {
                // Match the settings and term lock order used by lesson generation.
                $rules = $this->store->lockRules();
            } catch (SettingsNotFound) {
                $rules = null;
            }
            $term = $this->store->lockTerm($termId);
            $data->validate();
            $class = $this->store->createClass($term['id'], $data);
            if ($rules !== null) {
                $now = $this->clock->now();
                $today = Date::fromInstant($now, $rules->timezone);
                $until = $rules->generationEnd($today, $this->store->longestVisibleDays());
                $this->generator->fillLockedClass($class, $term, $today, $until, $rules->timezone, $now);
            }

            return $this->store->classDocument($class['id']);
        });
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

    private static function inUse(): CatalogFailure
    {
        return new CatalogFailure(409, 'the record is still in use', reason: 'in-use');
    }
}
