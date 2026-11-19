<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

final class PersonName
{
    private const array FOLD = [
        'ä' => 'ae', 'ö' => 'oe', 'ü' => 'ue', 'ß' => 'ss',
        'á' => 'a', 'à' => 'a', 'â' => 'a', 'ã' => 'a', 'å' => 'a',
        'é' => 'e', 'è' => 'e', 'ê' => 'e', 'ë' => 'e',
        'í' => 'i', 'ì' => 'i', 'î' => 'i', 'ï' => 'i',
        'ó' => 'o', 'ò' => 'o', 'ô' => 'o', 'õ' => 'o', 'ø' => 'o',
        'ú' => 'u', 'ù' => 'u', 'û' => 'u',
        'ç' => 'c', 'ñ' => 'n', 'ý' => 'y', 'ÿ' => 'y', '-' => ' ',
    ];

    public static function lower(string $text): string
    {
        return mb_convert_case($text, MB_CASE_LOWER_SIMPLE, 'UTF-8');
    }

    /** @return list<string> */
    public static function words(string $text): array
    {
        return preg_split('/[\p{Z}\x09-\x0D\x{85}]+/u', $text, flags: PREG_SPLIT_NO_EMPTY) ?: [];
    }

    public static function key(string $given, string $family): string
    {
        return implode(' ', self::words(strtr(self::lower($given . ' ' . $family), self::FOLD)));
    }
}
