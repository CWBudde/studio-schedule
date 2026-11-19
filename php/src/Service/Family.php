<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeZone;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\PersonData;
use StudioSchedule\Store\FamilyStore;
use Symfony\Component\Uid\Uuid;

final readonly class Family
{
    public function __construct(private FamilyStore $store, private ClockInterface $clock) {}

    /** @return array<string,mixed>|null */
    public function get(string $personId): ?array
    {
        return $this->store->inReadSnapshot(function () use ($personId): ?array {
            $person = $this->store->person($personId);

            return $person === null ? null : ['me' => $person, 'dependents' => $this->store->dependents($personId)];
        });
    }

    /** @return array<string,mixed>|null */
    public function addDependent(string $guardianId, #[SensitiveParameter] PersonData $person): ?array
    {
        return $this->store->inTransaction(function () use ($guardianId, $person): ?array {
            if (!$this->store->lockLivePersons($guardianId)) {
                return null;
            }
            $data = $this->validated($person);
            $id = Uuid::v7()->toRfc4122();
            $this->store->createDependent($id, $guardianId, $data);

            return $data->view($id);
        });
    }

    /** @return array<string,mixed>|'managed'|null */
    public function update(string $personId, #[SensitiveParameter] PersonData $person, ?string $guardianId = null): array|string|null
    {
        // Check membership before starting a transaction to avoid locking unrelated people.
        if ($guardianId !== null && !$this->store->isGuardian($guardianId, $personId)) {
            return null;
        }

        return $this->store->inTransaction(function () use ($guardianId, $personId, $person): array|string|null {
            $people = $guardianId === null ? [$personId] : [$guardianId, $personId];
            if (!$this->store->lockLivePersons(...$people)) {
                return null;
            }
            if ($guardianId !== null) {
                // The office can delete a guardian link without locking either person.
                if (!$this->store->isGuardian($guardianId, $personId, forUpdate: true)) {
                    return null;
                }
                if ($this->store->managed($personId)) {
                    return 'managed';
                }
            }
            $data = $this->validated($person);
            $this->store->updatePerson($personId, $data);

            return $data->view($personId);
        });
    }

    private function validated(#[SensitiveParameter] PersonData $data): PersonData
    {
        $given = SetupInput::trim($data->givenName);
        $family = SetupInput::trim($data->familyName);
        if ($given === '') {
            throw new FamilyFieldError('givenName', 'given name must not be empty');
        }
        if ($family === '') {
            throw new FamilyFieldError('familyName', 'family name must not be empty');
        }
        if (!$data->birthDate->isZero()) {
            $zone = new DateTimeZone($this->store->schoolTimezone());
            if ($data->birthDate->compare(Date::fromInstant($this->clock->now(), $zone)) > 0) {
                throw new FamilyFieldError('birthDate', 'birth date must not be in the future');
            }
        }

        return new PersonData($given, $family, SetupInput::trim($data->phone), $data->birthDate);
    }
}
