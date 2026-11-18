<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeZone;
use StudioSchedule\Domain\Currency;

final readonly class SetupInput
{
    public function __construct(
        public string $schoolName,
        public string $timezone,
        public string $locale,
        public string $currency,
        public string $adminEmail,
        public string $adminGivenName,
        public string $adminFamilyName,
    ) {}

    public function validated(): self
    {
        $school = self::name($this->schoolName, 'schoolName', 'School name');
        if (preg_match('/\A.{1,200}\z/us', $school) !== 1) {
            throw new InvalidSetupInput('schoolName', 'School name is too long');
        }

        $zone = self::trim($this->timezone);
        if (!in_array($zone, DateTimeZone::listIdentifiers(DateTimeZone::ALL_WITH_BC), true)) {
            throw new InvalidSetupInput('timezone', 'Choose an IANA time zone');
        }

        $tag = strtolower(str_replace('_', '-', self::trim($this->locale)));
        $locale = explode('-', $tag, 2)[0];
        if (!in_array($locale, ['en', 'de'], true)) {
            throw new InvalidSetupInput('defaultLocale', 'Default locale must be en or de');
        }

        // These are the non-ASCII characters whose Unicode uppercase is ASCII.
        $currency = strtoupper(strtr(self::trim($this->currency), ['ı' => 'I', 'ſ' => 'S']));
        if (Currency::exponent($currency) === null) {
            throw new InvalidSetupInput('currency', 'Currency must be an ISO 4217 code');
        }

        $givenName = self::name($this->adminGivenName, 'givenName', 'Given name');
        $familyName = self::name($this->adminFamilyName, 'familyName', 'Family name');
        $email = self::trim($this->adminEmail);
        if (!self::validEmail($email)) {
            throw new InvalidSetupInput('email', 'Enter a valid email address');
        }

        return new self($school, $zone, $locale, $currency, $email, $givenName, $familyName);
    }

    /** Match Go's strings.TrimSpace for fields and account lookup. */
    public static function trim(string $value): string
    {
        return preg_replace('/\A[\p{Z}\x09-\x0D\x{85}]+|[\p{Z}\x09-\x0D\x{85}]+\z/u', '', $value) ?? $value;
    }

    private static function name(string $value, string $field, string $label): string
    {
        $value = self::trim($value);
        if ($value === '') {
            throw new InvalidSetupInput($field, "$label must not be empty");
        }
        if (preg_match('/\A[^\x00]+\z/u', $value) !== 1) {
            throw new InvalidSetupInput($field, "$label must be valid UTF-8 text");
        }

        return $value;
    }

    private static function validEmail(string $email): bool
    {
        // Go accepts bare dot-atoms, including UTF-8, and IP literals. Its
        // exact address comparison rejects display names and quoted local parts.
        $parts = explode('@', $email, 2);
        if (count($parts) !== 2) {
            return false;
        }
        [$local, $domain] = $parts;
        if (!self::dotAtom($local)) {
            return false;
        }
        if (str_starts_with($domain, '[') && str_ends_with($domain, ']')) {
            $ip = substr($domain, 1, -1);
            if (str_starts_with($ip, 'IPv6:')) {
                return filter_var(substr($ip, 5), FILTER_VALIDATE_IP) !== false;
            }

            // Go's To4 accepts both IPv4 and IPv4-mapped IPv6.
            $address = inet_pton($ip);

            return $address !== false && (strlen($address) === 4 || str_starts_with($address, str_repeat("\0", 10) . "\xff\xff"));
        }

        return self::dotAtom($domain);
    }

    private static function dotAtom(string $value): bool
    {
        $atom = "[A-Za-z0-9!#\$%&'*+\/=?^_`{|}~\\x{80}-\\x{10ffff}-]++";

        return preg_match("/\\A$atom(?:\\.$atom)*\\z/u", $value) === 1;
    }
}
