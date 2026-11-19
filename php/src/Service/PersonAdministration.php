<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeZone;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\PersonData;
use StudioSchedule\Domain\PersonRecord;
use Symfony\Component\Uid\Uuid;

final readonly class PersonAdministration
{
    public function __construct(private PersonAdministrationPersistence $store, private ClockInterface $clock) {}

    /** @return array<string,mixed> */
    public function save(?string $id, #[SensitiveParameter] PersonRecord $record): array
    {
        return $this->store->inOfficeTransaction(function () use ($id, $record): array {
            if ($id !== null && !$this->store->lockLivePersons($id)) {
                throw self::notFound();
            }

            $profile = $record->profile;
            $given = SetupInput::trim($profile->givenName);
            $family = SetupInput::trim($profile->familyName);
            $email = SetupInput::trim($record->email);
            if ($given === '') {
                throw CatalogFailure::field('givenName', 'given name must not be empty');
            }
            if ($family === '') {
                throw CatalogFailure::field('familyName', 'family name must not be empty');
            }
            if ($email !== '' && !SetupInput::validEmail($email)) {
                throw CatalogFailure::field('email', 'invalid email');
            }
            if (!$profile->birthDate->isZero()) {
                $today = Date::fromInstant($this->clock->now(), new DateTimeZone($this->store->schoolTimezone()));
                if ($profile->birthDate->compare($today) > 0) {
                    throw CatalogFailure::field('birthDate', 'birth date must not be in the future');
                }
            }

            $clean = new PersonRecord(
                new PersonData($given, $family, SetupInput::trim($profile->phone), $profile->birthDate),
                $email,
                SetupInput::trim($record->notes),
                SetupInput::trim($record->memberNo),
            );
            $person = $id ?? Uuid::v7()->toRfc4122();
            $this->store->saveOfficePerson($person, $clean, create: $id === null);

            return $clean->view($person);
        });
    }

    /** @return array<string,mixed> */
    public function addGuardian(string $dependent, string $guardian): array
    {
        // A graph traversal can wait for overlapping edits. It must see the
        // newly committed links when it reads each person's dependents.
        return $this->store->inOfficeTransaction(function () use ($dependent, $guardian): array {
            if (!$this->store->lockLivePersons($dependent, $guardian)) {
                if (!$this->store->hasLivePerson($dependent)) {
                    throw self::notFound();
                }

                throw CatalogFailure::field('guardianId', 'person does not exist');
            }
            if ($guardian === $dependent) {
                throw CatalogFailure::field('guardianId', 'a person cannot be their own guardian');
            }
            $person = $this->store->personMatch($guardian) ?? throw self::notFound();
            $this->requireAcyclic($dependent, $guardian);
            $this->store->linkGuardian($guardian, $dependent);

            return $person;
        });
    }

    public function removeGuardian(string $dependent, string $guardian): void
    {
        $this->store->inOfficeTransaction(function () use ($dependent, $guardian): void {
            if (!$this->store->unlinkGuardian($guardian, $dependent)) {
                throw self::notFound();
            }
        });
    }

    private function requireAcyclic(string $dependent, string $guardian): void
    {
        $seen = [$dependent => true];
        $queue = [$dependent];
        for ($i = 0; $i < count($queue); ++$i) {
            foreach ($this->store->dependentIds($queue[$i]) as $child) {
                if ($child === $guardian) {
                    throw CatalogFailure::field('guardianId', 'a dependent of the person cannot be their guardian');
                }
                if (!isset($seen[$child])) {
                    $seen[$child] = true;
                    if ($this->store->hasLivePerson($child, lock: true)) {
                        $queue[] = $child;
                    }
                }
            }
        }
    }

    private static function notFound(): CatalogFailure
    {
        return new CatalogFailure(404, 'resource not found');
    }
}
