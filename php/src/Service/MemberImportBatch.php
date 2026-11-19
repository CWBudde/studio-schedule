<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use InvalidArgumentException;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\PersonName;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type ImportPerson from MemberImportPersistence
 * @phpstan-type GuardianResult array{slot:int,givenName:string,familyName:string,personId?:string,action:string,matchedBy?:string,linked:bool}
 * @phpstan-type GuardianPlan array{person:ImportPerson,result:GuardianResult}
 * @phpstan-type Duplicate array{id?:string,givenName:string,familyName:string,email?:string,memberNo?:string}
 * @phpstan-type ImportRow array{line:int,memberNo:string,givenName:string,familyName:string,personId?:string,action:string,
 *   changes:list<string>,issues:list<array{code:string,field:string}>,duplicates:list<Duplicate>,guardians:list<GuardianResult>}
 */
final class MemberImportBatch
{
    /** @var array<string,ImportPerson> */
    private array $people = [];
    /** @var array<array-key,string> */
    private array $numbers = [];
    /** @var array<string,array<string,true>> */
    private array $emails = [];
    /** @var array<string,array<string,true>> */
    private array $names = [];
    /** @var array<string,string> */
    private array $accounts = [];
    /** @var array<array-key,true> */
    private array $seen = [];

    public function __construct(private readonly MemberImportPersistence $store) {}

    /** @param list<array{line:int,values:array<string,string>}> $rows */
    public function lockCandidates(array $rows): void
    {
        $locked = [];
        do {
            $this->people = $this->numbers = $this->names = $this->emails = $this->accounts = [];
            foreach ($this->store->people() as $person) {
                $this->index($person);
            }
            $missing = array_diff_key($this->candidates($rows), $locked);
            foreach ($this->store->lockPeople(array_keys($missing)) as $id) {
                $locked[$id] = true;
            }
        } while ($missing !== []);
    }

    /**
     * @param list<array{line:int,values:array<string,string>}> $rows
     * @return array<string,true>
     */
    private function candidates(array $rows): array
    {
        $ids = [];
        foreach ($rows as $row) {
            $values = $row['values'];
            $id = $this->numbers[$values['memberNo']] ?? null;
            if ($id !== null) {
                $ids[$id] = true;
            }

            foreach ([1, 2] as $slot) {
                $prefix = 'guardian' . $slot;
                $email = PersonName::lower($values[$prefix . 'Email']);
                $account = $this->accounts[$email] ?? null;
                if ($account !== null) {
                    $ids[$account] = true;
                }
                $ids += $this->emails[$email] ?? [];
                $given = $values[$prefix . 'GivenName'];
                $family = $values[$prefix . 'FamilyName'];
                if ($given !== '' && $family !== '') {
                    $ids += $this->names[PersonName::key($given, $family)] ?? [];
                }
            }
        }

        return $ids;
    }

    /** @param ImportPerson $person */
    private function index(array $person): void
    {
        $id = $person['id'];
        $old = $this->people[$id] ?? null;
        if ($old !== null) {
            unset($this->numbers[$old['memberNo']], $this->emails[PersonName::lower($old['email'])][$id],
                $this->names[PersonName::key($old['givenName'], $old['familyName'])][$id]);
        }
        $this->people[$id] = $person;
        if ($person['memberNo'] !== '') {
            $this->numbers[$person['memberNo']] = $id;
        }
        if ($person['email'] !== '') {
            $this->emails[PersonName::lower($person['email'])][$id] = true;
        }
        if ($person['accountEmail'] !== '') {
            $address = PersonName::lower($person['accountEmail']);
            $other = $this->accounts[$address] ?? null;
            // Go's account list is ordered by ASCII-lowercased address.
            if ($other === null || strcmp(strtolower($this->people[$other]['accountEmail']), strtolower($person['accountEmail'])) <= 0) {
                $this->accounts[$address] = $id;
            }
        }
        $this->names[PersonName::key($person['givenName'], $person['familyName'])][$id] = true;
    }

    /**
     * @param array<string,string> $values
     * @return ImportRow
     */
    public function row(int $line, array $values, string $today): array
    {
        $no = $values['memberNo'];
        $out = ['line' => $line, 'memberNo' => $no, 'givenName' => $values['givenName'], 'familyName' => $values['familyName'],
            'action' => 'error', 'changes' => [], 'issues' => [], 'duplicates' => [], 'guardians' => []];
        $issue = static function (string $code, string $field) use (&$out): void {
            $out['issues'][] = ['code' => $code, 'field' => $field];
        };

        if ($no === '') {
            $issue('required', 'memberNo');
        } elseif (mb_strlen($no, 'UTF-8') > 40 || str_contains($no, "\0")) {
            $issue('invalid', 'memberNo');
        } elseif (isset($this->seen[$no])) {
            $issue('member-no-repeated', 'memberNo');
        } else {
            $this->seen[$no] = true;
        }

        $id = $this->numbers[$no] ?? null;
        $create = $id === null;
        $member = $id === null ? self::newPerson($no) : $this->people[$id];
        $changes = $this->fill($member, $values, $issue);
        if ($values['birthDate'] !== '') {
            try {
                $date = self::birthDate($values['birthDate']);
                if ($date > $today) {
                    $issue('future', 'birthDate');
                } elseif ($date !== $member['birthDate']) {
                    $member['birthDate'] = $date;
                    $changes[] = 'birthDate';
                }
            } catch (InvalidArgumentException) {
                $issue('invalid', 'birthDate');
            }
        }
        if ($create) {
            foreach (['givenName', 'familyName'] as $field) {
                if ($member[$field] === '') {
                    $issue('required', $field);
                }
            }
            array_unshift($changes, 'memberNo');
        }

        $guardians = [];
        foreach ([1, 2] as $slot) {
            $guardian = $this->guardian($slot, $values, $member['id'], $create, $issue);
            if ($guardian !== null) {
                $guardians[$guardian['person']['id']] ??= $guardian;
            }
        }
        if ($out['issues'] !== []) {
            return $out;
        }

        if ($create) {
            $out['duplicates'] = $this->duplicates($member, $guardians);
        }

        $out['action'] = $create ? 'create' : ($changes === [] ? 'unchanged' : 'update');
        $out['changes'] = $changes;
        $out['personId'] = $member['id'];
        $out['givenName'] = $member['givenName'];
        $out['familyName'] = $member['familyName'];
        $this->save($member, $out['action']);
        foreach ($guardians as $guardian) {
            $result = $guardian['result'];
            $this->save($guardian['person'], $result['action']);
            if ($result['linked']) {
                $this->store->link($guardian['person']['id'], $member['id']);
            }
            $out['guardians'][] = $result;
        }

        return $out;
    }

    /**
     * @param ImportPerson $person
     * @param array<string,string> $values
     * @param callable(string,string):void $issue
     * @return list<string>
     */
    private function fill(array &$person, array $values, callable $issue, string $prefix = ''): array
    {
        $changes = [];
        foreach (['givenName' => 100, 'familyName' => 100, 'email' => 254, 'phone' => 50, 'notes' => 2000] as $field => $limit) {
            $key = $prefix === '' ? $field : $prefix . ucfirst($field);
            $value = $values[$key] ?? '';
            if ($value === '') {
                continue;
            }
            if (str_contains($value, "\0") || ($field === 'email' && !SetupInput::validEmail($value))) {
                $issue('invalid', $key);
                continue;
            }
            if ($value === $person[$field] || ($field === 'email'
                && mb_convert_case($value, MB_CASE_FOLD_SIMPLE, 'UTF-8') === mb_convert_case($person['email'], MB_CASE_FOLD_SIMPLE, 'UTF-8'))) {
                continue;
            }
            if (mb_strlen($value, 'UTF-8') > $limit) {
                $issue('invalid', $key);
                continue;
            }
            $person[$field] = $value;
            $changes[] = $field;
        }

        return $changes;
    }

    private static function birthDate(string $value): string
    {
        if (preg_match('/\A([0-9]{1,2})\.([0-9]{1,2})\.([0-9]{4})\z/', $value, $parts) === 1) {
            $value = $parts[3] . '-' . str_pad($parts[2], 2, '0', STR_PAD_LEFT) . '-' . str_pad($parts[1], 2, '0', STR_PAD_LEFT);
        } elseif (preg_match('/\A([0-9]{4})-([0-9]{1,2})-([0-9]{1,2})\z/', $value, $parts) === 1) {
            $value = $parts[1] . '-' . str_pad($parts[2], 2, '0', STR_PAD_LEFT) . '-' . str_pad($parts[3], 2, '0', STR_PAD_LEFT);
        }

        return (string) Date::parse($value);
    }

    /**
     * @param array<string,string> $values
     * @param callable(string,string):void $issue
     * @return GuardianPlan|null
     */
    private function guardian(int $slot, array $values, string $member, bool $newMember, callable $issue): ?array
    {
        $prefix = 'guardian' . $slot;
        $given = $values[$prefix . 'GivenName'];
        $family = $values[$prefix . 'FamilyName'];
        $email = $values[$prefix . 'Email'];
        if ($given === '' && $family === '' && $email === '' && $values[$prefix . 'Phone'] === '') {
            return null;
        }

        [$id, $matchedBy, $ambiguous] = $this->matchGuardian($given, $family, $email);
        if ($ambiguous) {
            $issue('guardian-ambiguous', $prefix . ($email === '' ? 'GivenName' : 'Email'));

            return null;
        }
        if ($id === $member) {
            $issue('guardian-is-member', $prefix . 'GivenName');

            return null;
        }
        $person = $id === null ? self::newPerson('') : $this->people[$id];
        if ($id === null) {
            if ($given === '') {
                $issue('required', $prefix . 'GivenName');
            }
            if ($family === '') {
                $issue('required', $prefix . 'FamilyName');
            }
        }
        $changes = $this->fill($person, $values, $issue, $prefix);
        $linked = $newMember || $id === null || !$this->store->linked($id, $member);
        if ($linked && !$newMember && $id !== null && $this->isDependentOf($id, $member)) {
            $issue('guardian-cycle', $prefix . 'GivenName');
        }

        return ['person' => $person, 'result' => [
            'slot' => $slot, 'givenName' => $person['givenName'], 'familyName' => $person['familyName'],
            'personId' => $person['id'], 'action' => $id === null ? 'create' : ($changes === [] ? 'unchanged' : 'update'),
            'linked' => $linked,
        ] + ($matchedBy === '' ? [] : ['matchedBy' => $matchedBy])];
    }

    /** @return array{?string,string,bool} */
    private function matchGuardian(string $given, string $family, string $email): array
    {
        $name = PersonName::key($given, $family);
        $named = $given !== '' && $family !== '';
        $address = PersonName::lower($email);
        if ($address !== '') {
            $account = $this->accounts[$address] ?? null;
            if ($account !== null && (!$named || isset($this->names[$name][$account]))) {
                return [$account, 'email', false];
            }
            $matches = $this->emails[$address] ?? [];
            if ($named) {
                $matches = array_intersect_key($matches, $this->names[$name] ?? []);
            }
            if ($matches !== []) {
                return self::match($matches, 'email');
            }
        }
        if (!$named) {
            return [null, '', false];
        }
        $matches = $this->names[$name] ?? [];
        if ($address !== '') {
            foreach (array_keys($matches) as $id) {
                $person = $this->people[$id];
                $other = PersonName::lower($person['accountEmail'] !== '' ? $person['accountEmail'] : $person['email']);
                if ($other !== '' && $other !== $address) {
                    unset($matches[$id]);
                }
            }
        }

        return self::match($matches, 'name');
    }

    /**
     * @param array<string,true> $matches
     * @return array{?string,string,bool}
     */
    private static function match(array $matches, string $method): array
    {
        return match (count($matches)) {
            0 => [null, '', false],
            1 => [array_key_first($matches), $method, false],
            default => [null, '', true],
        };
    }

    private function isDependentOf(string $person, string $ancestor): bool
    {
        $seen = [$ancestor => true];
        $queue = [$ancestor];
        for ($index = 0; $index < count($queue); ++$index) {
            foreach ($this->store->dependents($queue[$index]) as $dependent) {
                if ($dependent === $person) {
                    return true;
                }
                if (!isset($seen[$dependent])) {
                    $seen[$dependent] = true;
                    if ($this->store->lockPeople([$dependent]) !== []) {
                        $queue[] = $dependent;
                    }
                }
            }
        }

        return false;
    }

    /**
     * @param ImportPerson $member
     * @param array<string,GuardianPlan> $guardians
     * @return list<Duplicate>
     */
    private function duplicates(array $member, array $guardians): array
    {
        $family = [];
        foreach ($guardians as $guardian) {
            $id = $guardian['person']['id'];
            $family[$id] = true;
            if ($guardian['result']['action'] !== 'create') {
                $family += array_fill_keys($this->store->dependents($id), true);
            }
        }
        $found = [];
        $email = PersonName::lower($member['email']);
        if ($email !== '') {
            $matches = $this->emails[$email] ?? [];
            if (isset($this->accounts[$email])) {
                $matches[$this->accounts[$email]] = true;
            }
            $found = array_diff_key($matches, $family);
        }
        if ($member['birthDate'] !== '') {
            $name = PersonName::key($member['givenName'], $member['familyName']);
            foreach (array_keys($this->names[$name] ?? []) as $id) {
                if ($this->people[$id]['birthDate'] === $member['birthDate']) {
                    $found[$id] = true;
                }
            }
        }
        ksort($found, SORT_STRING);
        $out = [];
        foreach (array_keys($found) as $id) {
            $person = $this->people[$id];
            $out[] = ['id' => $id, 'givenName' => $person['givenName'], 'familyName' => $person['familyName']]
                + ($person['email'] === '' ? [] : ['email' => $person['email']])
                + ($person['memberNo'] === '' ? [] : ['memberNo' => $person['memberNo']]);
        }

        return $out;
    }

    /** @param ImportPerson $person */
    private function save(array $person, string $action): void
    {
        if ($action !== 'unchanged') {
            $this->store->save($person, $action === 'create');
            $this->index($person);
        }
    }

    /** @return ImportPerson */
    private static function newPerson(string $memberNo): array
    {
        return ['id' => Uuid::v7()->toRfc4122(), 'memberNo' => $memberNo, 'givenName' => '', 'familyName' => '',
            'email' => '', 'phone' => '', 'birthDate' => '', 'notes' => '', 'accountEmail' => ''];
    }
}
