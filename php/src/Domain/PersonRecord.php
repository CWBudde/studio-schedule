<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

final readonly class PersonRecord
{
    public function __construct(
        public PersonData $profile,
        public string $email,
        public string $notes,
        public string $memberNo,
    ) {}

    /** @return array<string,mixed> */
    public function view(string $id): array
    {
        return $this->profile->view($id) + array_filter([
            'email' => $this->email, 'notes' => $this->notes, 'memberNo' => $this->memberNo,
        ], static fn(string $value): bool => $value !== '');
    }
}
