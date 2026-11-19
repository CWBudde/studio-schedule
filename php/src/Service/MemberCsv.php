<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use InvalidArgumentException;
use SensitiveParameter;
use StudioSchedule\Mail\Formatting;

final readonly class MemberCsv
{
    public const int MAX_BYTES = 1_048_576;
    private const int MAX_ROWS = 5_000;
    private const int MAX_COLUMNS = 100;

    /**
     * @param list<string> $headers
     * @param list<array{line:int,cells:list<string>}> $rows
     */
    private function __construct(
        public string $encoding,
        public string $delimiter,
        public array $headers,
        public array $rows,
    ) {}

    public static function parse(#[SensitiveParameter] string $bytes): self
    {
        if (strlen($bytes) > self::MAX_BYTES) {
            throw new InvalidArgumentException('the file is larger than 1024 KiB');
        }
        if (str_starts_with($bytes, "\xEF\xBB\xBF")) {
            $bytes = substr($bytes, 3);
        }

        $encoding = mb_check_encoding($bytes, 'UTF-8') ? 'utf-8' : 'windows-1252';
        $text = $bytes;
        if ($encoding === 'windows-1252') {
            // Go's Windows-1252 decoder replaces these five undefined bytes.
            $text = str_replace(
                ["\u{0081}", "\u{008D}", "\u{008F}", "\u{0090}", "\u{009D}"],
                "\u{FFFD}",
                mb_convert_encoding($bytes, 'UTF-8', 'Windows-1252'),
            );
        }
        if (Formatting::trimWhitespace($text) === '') {
            throw new InvalidArgumentException('the file is empty');
        }
        $delimiter = self::detectDelimiter($text);
        $text = str_replace("\r\n", "\n", $text);
        if (str_ends_with($text, "\r")) {
            $text = substr($text, 0, -1);
        }

        $headers = $rows = [];
        $offset = 0;
        $line = 1;
        $length = strlen($text);
        while ($offset < $length) {
            if ($text[$offset] === "\n") {
                ++$offset;
                ++$line;
                continue;
            }

            $startLine = $line;
            $cells = self::record($text, $delimiter, $offset, $line);
            if ($headers === []) {
                self::validateHeaders($cells);
                $headers = $cells;
                continue;
            }

            if (self::emptyRecord($cells)) {
                continue;
            }
            $columns = count($headers);
            if (!self::emptyRecord(array_slice($cells, $columns))) {
                throw new InvalidArgumentException("line $startLine has more cells than the header names");
            }
            if (count($rows) === self::MAX_ROWS) {
                throw new InvalidArgumentException('the file has more than 5000 rows');
            }
            $rows[] = ['line' => $startLine, 'cells' => array_pad(array_slice($cells, 0, $columns), $columns, '')];
        }

        return new self($encoding, $delimiter, $headers, $rows);
    }

    private static function detectDelimiter(#[SensitiveParameter] string $text): string
    {
        $counts = [';' => 0, ',' => 0, "\t" => 0];
        $quoted = false;
        for ($offset = 0, $length = strlen($text); $offset < $length; ++$offset) {
            $character = $text[$offset];
            if ($character === '"') {
                $quoted = !$quoted;
            } elseif (!$quoted) {
                if ($character === "\n") {
                    break;
                }
                if (isset($counts[$character])) {
                    ++$counts[$character];
                }
            }
        }
        $highest = max($counts);
        foreach ($counts as $delimiter => $count) {
            if ($count === $highest) {
                return $delimiter;
            }
        }

        throw new \LogicException('Missing CSV delimiter candidates');
    }

    /** @return list<string> */
    private static function record(#[SensitiveParameter] string $text, string $delimiter, int &$offset, int &$line): array
    {
        $cells = [];
        do {
            if (($text[$offset] ?? '') === '"') {
                $value = self::quotedCell($text, $delimiter, $offset);
                $line += substr_count($value, "\n");
            } else {
                $width = strcspn($text, $delimiter . "\n", $offset);
                $value = substr($text, $offset, $width);
                $offset += $width;
            }
            $cells[] = Formatting::trimWhitespace($value);
            $next = $text[$offset] ?? '';
            ++$offset;
        } while ($next === $delimiter);
        if ($next === "\n") {
            ++$line;
        }

        return $cells;
    }

    private static function quotedCell(#[SensitiveParameter] string $text, string $delimiter, int &$offset): string
    {
        ++$offset;
        $cell = '';
        while (($quote = strpos($text, '"', $offset)) !== false) {
            $cell .= substr($text, $offset, $quote - $offset);
            $offset = $quote + 1;
            $next = $text[$offset] ?? '';
            if ($next === $delimiter || $next === "\n" || $next === '') {
                return $cell;
            }

            // Spreadsheet exports may leave a single quote inside a quoted field.
            $cell .= '"';
            if ($next === '"') {
                ++$offset;
            }
        }
        $cell .= substr($text, $offset);
        $offset = strlen($text);

        return $cell;
    }

    /** @param list<string> $headers */
    private static function validateHeaders(array $headers): void
    {
        if (count($headers) > self::MAX_COLUMNS) {
            throw new InvalidArgumentException('the file has more than 100 columns');
        }
        $seen = [];
        foreach ($headers as $index => $name) {
            if ($name === '') {
                throw new InvalidArgumentException('column ' . ($index + 1) . ' of the header has no name');
            }
            $key = mb_convert_case($name, MB_CASE_LOWER_SIMPLE, 'UTF-8');
            if (isset($seen[$key])) {
                throw new InvalidArgumentException("the header names the column \"$name\" twice");
            }
            $seen[$key] = true;
        }
    }

    /** @param list<string> $cells */
    private static function emptyRecord(array $cells): bool
    {
        foreach ($cells as $cell) {
            if ($cell !== '') {
                return false;
            }
        }

        return true;
    }
}
