<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

use JsonSerializable;

/** An integer amount in the minor units of the school's currency. */
final readonly class Money implements JsonSerializable
{
    private const string SPACE = "\u{00a0}";
    private const array SYMBOLS = ['EUR' => '€', 'USD' => '$', 'GBP' => '£'];

    public function __construct(public int $minorUnits) {}

    public function jsonSerialize(): int
    {
        return $this->minorUnits;
    }

    public function format(string $currency, string $locale): string
    {
        $amount = $this->minorUnits;
        $exponent = Currency::exponent($currency) ?? 2;
        $german = $locale === 'de';
        // Keep the amount in decimal digits to retain the full minor-unit precision.
        $digits = str_pad(ltrim((string) $amount, '-'), 1 + $exponent, '0', STR_PAD_LEFT);
        $whole = substr($digits, 0, strlen($digits) - $exponent);
        $number = (string) preg_replace('/\B(?=(?:[0-9]{3})+\z)/', $german ? '.' : ',', $whole);
        if ($exponent > 0) {
            $number .= ($german ? ',' : '.') . substr($digits, -$exponent);
        }
        $symbol = self::SYMBOLS[$currency] ?? $currency;
        $signed = $amount < 0 ? '−' : '';
        if ($german) {
            return $signed . $number . self::SPACE . $symbol;
        }

        return $signed . $symbol . (isset(self::SYMBOLS[$currency]) ? '' : self::SPACE) . $number;
    }
}
