<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

final class Currency
{
    // Match the ISO 4217 exponents in internal/domain/currency.go.
    private const array CODES = [
        0 => ' BIF CLP DJF GNF ISK JPY KMF KRW PYG RWF UGX UYI VND VUV XAF XOF XPF ',
        2 => ' AED AFN ALL AMD AOA ARS AUD AWG AZN BAM BBD BDT BGN BMD BND BOB BOV BRL BSD BTN BWP BYN BZD '
            . 'CAD CDF CHE CHF CHW CNY COP COU CRC CUP CVE CZK DKK DOP DZD EGP ERN ETB EUR FJD FKP GBP GEL GHS GIP '
            . 'GMD GTQ GYD HKD HNL HTG HUF IDR ILS INR IRR JMD KES KGS KHR KPW KYD KZT LAK LBP LKR LRD LSL MAD MDL '
            . 'MGA MKD MMK MNT MOP MRU MUR MVR MWK MXN MXV MYR MZN NAD NGN NIO NOK NPR NZD PAB PEN PGK PHP PKR PLN '
            . 'QAR RON RSD RUB SAR SBD SCR SDG SEK SGD SHP SLE SOS SRD SSP STN SVC SYP SZL THB TJS TMT TOP TRY TTD '
            . 'TWD TZS UAH USD USN UYU UZS VED VES WST XCD XCG YER ZAR ZMW ZWG ',
        3 => ' BHD IQD JOD KWD LYD OMR TND ',
        4 => ' CLF UYW ',
    ];

    public static function exponent(string $code): ?int
    {
        if (strlen($code) !== 3 || str_contains($code, ' ')) {
            return null;
        }

        foreach (self::CODES as $exponent => $codes) {
            if (str_contains($codes, ' ' . $code . ' ')) {
                return $exponent;
            }
        }

        return null;
    }
}
