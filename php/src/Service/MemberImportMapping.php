<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use StudioSchedule\Domain\PersonName;

final readonly class MemberImportMapping
{
    public const array FIELDS = [
        'memberNo', 'givenName', 'familyName', 'email', 'phone', 'birthDate', 'notes',
        'guardian1GivenName', 'guardian1FamilyName', 'guardian1Email', 'guardian1Phone',
        'guardian2GivenName', 'guardian2FamilyName', 'guardian2Email', 'guardian2Phone',
    ];

    /**
     * @param array<string,int> $columns
     * @param array<int,string> $notes
     */
    private function __construct(private array $columns, private array $notes) {}

    /**
     * @param list<string> $headers
     * @param array<array-key,mixed> $mapping
     * @param list<mixed> $notes
     */
    public static function resolve(array $headers, array $mapping, array $notes): self
    {
        $known = array_combine(array_map(strtolower(...), self::FIELDS), self::FIELDS);
        $names = [];
        foreach ($mapping as $field => $header) {
            $target = $known[strtolower((string) $field)] ?? null;
            if ($target === null) {
                throw new MemberImportProblem('mapping.columns', 'unknown target field');
            }
            if (!is_string($header) || mb_strlen($header, 'UTF-8') > 200) {
                throw new MemberImportProblem('mapping.columns.' . $target, 'expected a header of at most 200 characters');
            }
            $names[$target] = SetupInput::trim($header);
        }
        $indices = array_flip(array_map(PersonName::lower(...), $headers));
        $columns = $noteColumns = $used = [];
        foreach (self::FIELDS as $field) {
            if (($names[$field] ?? '') !== '') {
                $columns[$field] = self::takeColumn($indices, $used, $names[$field], 'mapping.columns.' . $field);
            }
        }
        if (count($notes) > 100) {
            throw new MemberImportProblem('mapping.notes', 'expected at most 100 note columns');
        }
        foreach ($notes as $header) {
            if (!is_string($header)) {
                throw new MemberImportProblem('mapping.notes', 'expected header names as strings');
            }
            $index = self::takeColumn($indices, $used, SetupInput::trim($header), 'mapping.notes');
            $noteColumns[$index] = $headers[$index];
        }
        $required = ['memberNo', 'givenName', 'familyName'];
        foreach ([1, 2] as $slot) {
            $prefix = 'guardian' . $slot;
            if (isset($columns[$prefix . 'GivenName']) || isset($columns[$prefix . 'FamilyName'])
                || isset($columns[$prefix . 'Email']) || isset($columns[$prefix . 'Phone'])) {
                $required[] = $prefix . 'GivenName';
                $required[] = $prefix . 'FamilyName';
            }
        }
        foreach ($required as $field) {
            if (!isset($columns[$field])) {
                throw new MemberImportProblem('mapping.columns.' . $field, 'the field must be mapped');
            }
        }

        return new self($columns, $noteColumns);
    }

    /**
     * @param list<string> $cells
     * @return array<string,string>
     */
    public function values(array $cells): array
    {
        $values = array_fill_keys(self::FIELDS, '');
        foreach ($this->columns as $field => $index) {
            $values[$field] = $cells[$index];
        }
        $notes = $values['notes'] === '' ? [] : [$values['notes']];
        foreach ($this->notes as $index => $header) {
            if ($cells[$index] !== '') {
                $notes[] = $header . ': ' . $cells[$index];
            }
        }
        $values['notes'] = implode("\n", $notes);

        return $values;
    }

    /**
     * @param array<array-key,int> $indices
     * @param array<int,true> $used
     */
    private static function takeColumn(array $indices, array &$used, string $header, string $field): int
    {
        $index = $indices[PersonName::lower($header)] ?? null;
        if ($index === null) {
            throw new MemberImportProblem($field, "the file has no column \"$header\"");
        }
        if (isset($used[$index])) {
            throw new MemberImportProblem($field, "the column \"$header\" is mapped twice");
        }
        $used[$index] = true;

        return $index;
    }
}
