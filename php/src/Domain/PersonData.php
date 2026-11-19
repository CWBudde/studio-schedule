<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

final readonly class PersonData
{
    public function __construct(
        public string $givenName,
        public string $familyName,
        public string $phone,
        public Date $birthDate,
    ) {}

    /** @return array{id:string,givenName:string,familyName:string,phone?:string,birthDate?:string} */
    public function view(string $id): array
    {
        $person = ['id' => $id, 'givenName' => $this->givenName, 'familyName' => $this->familyName];
        if ($this->phone !== '') {
            $person['phone'] = $this->phone;
        }
        if (!$this->birthDate->isZero()) {
            $person['birthDate'] = (string) $this->birthDate;
        }

        return $person;
    }
}
