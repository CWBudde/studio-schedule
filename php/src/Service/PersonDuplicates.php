<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use StudioSchedule\Domain\PersonName;

/**
 * @phpstan-import-type ImportPerson from MemberImportPersistence
 */
final readonly class PersonDuplicates
{
    private const int LIMIT = 200;

    public function __construct(private DuplicatePersistence $store) {}

    /** @return list<array{a:array<string,string>,b:array<string,string>,strength:string,reasons:list<string>}> */
    public function list(): array
    {
        $snapshot = $this->store->snapshot();
        $people = $snapshot['people'];
        $emails = $dated = $names = $undated = $nameOf = [];
        foreach ($people as $i => $person) {
            foreach ([$person['email'], $person['accountEmail']] as $email) {
                if ($email !== '') {
                    $emails[PersonName::lower($email)][$i] = true;
                }
            }
            $name = PersonName::key($person['givenName'], $person['familyName']);
            $nameOf[$i] = $name;
            $names[$name][$i] = true;
            if ($person['birthDate'] === '') {
                $undated[$name][$i] = true;
            } else {
                $dated[$name][$person['birthDate']][$i] = true;
            }
        }

        $strong = $weak = [];
        foreach ($people as $i => $left) {
            $name = $nameOf[$i];
            $date = $left['birthDate'];
            $emailMatches = ($emails[PersonName::lower($left['email'])] ?? []) + ($emails[PersonName::lower($left['accountEmail'])] ?? []);
            $dateMatches = $dated[$name][$date] ?? [];
            $nameMatches = $date === '' ? $names[$name] : ($undated[$name] ?? []);

            // Keep the directory order within each strength and cap the accumulated pairs.
            $matches = $emailMatches + $dateMatches + (count($weak) < self::LIMIT ? $nameMatches : []);
            ksort($matches, SORT_NUMERIC);
            foreach ($matches as $j => $_) {
                if ($j <= $i) {
                    continue;
                }
                $right = $people[$j];
                if (isset($snapshot['dismissed'][min($left['id'], $right['id'])][max($left['id'], $right['id'])])) {
                    continue;
                }
                $reasons = [];
                if (isset($emailMatches[$j]) && !self::related($left['id'], $right['id'], $snapshot['guardians'])) {
                    $reasons[] = 'email';
                }
                $isStrong = $reasons !== [] || isset($dateMatches[$j]);
                if (isset($nameMatches[$j])) {
                    $reasons[] = 'name';
                }
                if (isset($dateMatches[$j])) {
                    $reasons[] = 'name-birth-date';
                }
                if ($reasons === [] || (!$isStrong && count($weak) >= self::LIMIT)) {
                    continue;
                }

                $pair = ['a' => self::candidate($left), 'b' => self::candidate($right),
                    'strength' => $isStrong ? 'strong' : 'weak', 'reasons' => $reasons];
                if ($isStrong) {
                    $strong[] = $pair;
                    if (count($strong) === self::LIMIT) {
                        return $strong;
                    }
                } else {
                    $weak[] = $pair;
                }
            }
        }

        return [...$strong, ...array_slice($weak, 0, self::LIMIT - count($strong))];
    }

    public function dismiss(string $a, string $b, string $actor): bool
    {
        return $this->store->saveDismissal(min($a, $b), max($a, $b), $actor);
    }

    /** @param array<string,array<string,true>> $guardians */
    private static function related(string $a, string $b, array $guardians): bool
    {
        $ofA = $guardians[$a] ?? [];
        $ofB = $guardians[$b] ?? [];

        return isset($ofA[$b]) || isset($ofB[$a]) || array_intersect_key($ofA, $ofB) !== [];
    }

    /**
     * @param ImportPerson $person
     * @return array<string,string>
     */
    private static function candidate(array $person): array
    {
        return array_filter($person, static fn(string $value): bool => $value !== '');
    }
}
