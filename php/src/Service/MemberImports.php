<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Domain\PersonName;

/**
 * @phpstan-import-type Counts from MemberImportPersistence
 * @phpstan-import-type ImportRow from MemberImportBatch
 */
final readonly class MemberImports
{
    public function __construct(private MemberImportPersistence $store, private ClockInterface $clock) {}

    /** @return array{applied:bool,counts:Counts,rows:list<ImportRow>,runId?:string} */
    public function run(
        string $name,
        #[SensitiveParameter]
        MemberCsv $csv,
        MemberImportMapping $mapping,
        bool $apply,
        ?string $actor,
    ): array {
        $name = SetupInput::trim($name);
        if (str_contains($name, "\0")) {
            throw new MemberImportProblem('fileName', 'expected a file name without null bytes');
        }
        $rows = array_map(static fn(array $row): array => [
            'line' => $row['line'], 'values' => $mapping->values($row['cells']),
        ], $csv->rows);

        try {
            return $this->store->write(function () use ($rows, $name, $apply, $actor): array {
                $start = $this->clock->now();
                $batch = new MemberImportBatch($this->store);
                $batch->lockCandidates($rows);
                $today = $this->clock->now()->setTimezone($this->store->schoolZone())->format('Y-m-d');
                $results = $errors = [];
                $counts = ['rows' => count($rows), 'created' => 0, 'updated' => 0, 'unchanged' => 0, 'failed' => 0,
                    'guardiansCreated' => 0, 'guardiansUpdated' => 0, 'linksAdded' => 0];
                foreach ($rows as $row) {
                    $result = $batch->row($row['line'], $row['values'], $today);
                    $results[] = $result;
                    $this->count($counts, $result);
                    if ($result['action'] === 'error' && count($errors) < 200) {
                        $errors[] = ['line' => $result['line']] + $result['issues'][0];
                    }
                }
                if (!$apply) {
                    throw new MemberImportPreview(['applied' => false, 'counts' => $counts, 'rows' => self::previewRows($results)]);
                }
                $id = $this->store->record($name === '' ? 'import.csv' : $name, $actor, $start, max($start, $this->clock->now()), $counts, $errors);

                return ['applied' => true, 'counts' => $counts, 'rows' => $results, 'runId' => $id];
            });
        } catch (MemberImportPreview $preview) {
            return $preview->result;
        }
    }

    /** @return list<array<string,mixed>> */
    public function runs(): array
    {
        return $this->store->runs();
    }

    /** @return list<array<string,string>> */
    public function searchPeople(string $query): array
    {
        $words = PersonName::words(PersonName::lower($query));
        $matches = [];
        foreach ($this->store->searchPeople() as $person) {
            $text = PersonName::lower($person['givenName'] . ' ' . $person['familyName'] . ' ' . $person['email'] . ' ' . $person['memberNo']);
            foreach ($words as $word) {
                if (!str_contains($text, $word)) {
                    continue 2;
                }
            }
            $matches[] = ['id' => $person['id'], 'givenName' => $person['givenName'], 'familyName' => $person['familyName']]
                + ($person['email'] === '' ? [] : ['email' => $person['email']])
                + ($person['memberNo'] === '' ? [] : ['memberNo' => $person['memberNo']]);
            if (count($matches) === 50) {
                break;
            }
        }

        return $matches;
    }

    /**
     * @param Counts $counts
     * @param ImportRow $row
     */
    private function count(array &$counts, array $row): void
    {
        ++$counts[match ($row['action']) {
            'create' => 'created',
            'update' => 'updated',
            'unchanged' => 'unchanged',
            default => 'failed',
        }];
        foreach ($row['guardians'] as $guardian) {
            if ($guardian['action'] === 'create') {
                ++$counts['guardiansCreated'];
            } elseif ($guardian['action'] === 'update') {
                ++$counts['guardiansUpdated'];
            }
            if ($guardian['linked']) {
                ++$counts['linksAdded'];
            }
        }
    }

    /**
     * @param list<ImportRow> $rows
     * @return list<ImportRow>
     */
    private static function previewRows(array $rows): array
    {
        $created = [];
        foreach ($rows as $row) {
            if ($row['action'] === 'create' && isset($row['personId'])) {
                $created[$row['personId']] = true;
            }
            foreach ($row['guardians'] as $guardian) {
                if ($guardian['action'] === 'create' && isset($guardian['personId'])) {
                    $created[$guardian['personId']] = true;
                }
            }
        }
        return array_map(static function (array $row) use ($created): array {
            if (isset($row['personId'], $created[$row['personId']])) {
                unset($row['personId']);
            }
            foreach ($row['guardians'] as $index => $guardian) {
                if (isset($guardian['personId'], $created[$guardian['personId']])) {
                    unset($row['guardians'][$index]['personId']);
                }
            }
            foreach ($row['duplicates'] as $index => $duplicate) {
                if (isset($duplicate['id'], $created[$duplicate['id']])) {
                    unset($row['duplicates'][$index]['id']);
                }
            }

            return $row;
        }, $rows);
    }
}
