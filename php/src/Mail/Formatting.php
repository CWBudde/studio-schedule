<?php

declare(strict_types=1);

namespace StudioSchedule\Mail;

use DateTimeInterface;

final class Formatting
{
    private const string WHITESPACE = '[\p{Z}\x09-\x0d\x{0085}]';

    private const array GERMAN_WEEKDAYS = ['Sonntag', 'Montag', 'Dienstag', 'Mittwoch', 'Donnerstag', 'Freitag', 'Samstag'];
    private const array GERMAN_MONTHS = [
        'Januar', 'Februar', 'März', 'April', 'Mai', 'Juni',
        'Juli', 'August', 'September', 'Oktober', 'November', 'Dezember',
    ];

    public static function date(DateTimeInterface $instant, string $locale): string
    {
        $year = (int) $instant->format('Y');
        if ($locale === 'de') {
            $weekday = self::GERMAN_WEEKDAYS[(int) $instant->format('w')];
            $month = self::GERMAN_MONTHS[(int) $instant->format('n') - 1];

            return sprintf('%s, %s. %s %d', $weekday, $instant->format('j'), $month, $year);
        }

        return $instant->format('l, j F ') . $year;
    }

    public static function clock(DateTimeInterface $instant): string
    {
        return $instant->format('H:i');
    }

    public static function trimWhitespace(string $value): string
    {
        return preg_replace('/\A' . self::WHITESPACE . '+|' . self::WHITESPACE . '+\z/u', '', $value) ?? trim($value);
    }

    public static function singleLine(string $value): string
    {
        return trim(preg_replace('/' . self::WHITESPACE . '+/u', ' ', $value) ?? preg_replace('/\s+/', ' ', $value) ?? '');
    }

    /** Uses the Go HTML template entities for quoted attributes and text nodes. */
    public static function escapeHtml(string $value): string
    {
        return strtr($value, [
            "\0" => "\u{fffd}",
            '"' => '&#34;',
            '&' => '&amp;',
            "'" => '&#39;',
            '+' => '&#43;',
            '<' => '&lt;',
            '>' => '&gt;',
        ]);
    }

    /** Normalizes URL bytes as Go's html/template does before escaping the attribute. */
    public static function url(string $url): string
    {
        $scheme = explode(':', $url, 2);
        if (count($scheme) === 2 && !str_contains($scheme[0], '/')
            && !in_array(strtolower($scheme[0]), ['http', 'https', 'mailto'], true)
        ) {
            return '#ZgotmplZ';
        }

        return preg_replace_callback(
            '~%(?![0-9a-fA-F]{2})|[^A-Za-z0-9!#$&*+,/:;=?@\[\]_.%\~-]~',
            static fn(array $match): string => sprintf('%%%02x', ord($match[0])),
            $url,
        ) ?? '#ZgotmplZ';
    }
}
