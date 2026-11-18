<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

final class CommonPasswords
{
    public const int MIN_LENGTH = 8;

    /** @var array<int|string, true>|null */
    private ?array $entries = null;

    public function __construct(private readonly string $path) {}

    public function isCommon(#[\SensitiveParameter] string $password): bool
    {
        $this->entries ??= $this->load();
        // The shared list is ASCII. Go's Unicode lowercase maps these two additional characters to ASCII.
        $lowercase = strtolower(strtr($password, ['İ' => 'i', 'K' => 'k']));

        return isset($this->entries[$lowercase]);
    }

    /** @return array<int|string, true> */
    private function load(): array
    {
        $lines = @file($this->path, FILE_IGNORE_NEW_LINES);
        if ($lines === false) {
            throw new \RuntimeException('Unable to read the common-password list');
        }

        $entries = [];
        foreach ($lines as $line) {
            if (strlen($line) >= self::MIN_LENGTH) {
                $entries[strtolower($line)] = true;
            }
        }
        if ($entries === []) {
            throw new \RuntimeException('The common-password list has no usable entries');
        }

        return $entries;
    }
}
