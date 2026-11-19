<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

final class MemberImportHeaders
{
    private const array SYNONYMS = [
        'memberNo' => ['memberno', 'membernumber', 'memberid', 'membershipnumber', 'membershipno', 'mitgliedsnummer',
            'mitgliedsnr', 'mitgliednr', 'mitgliedernummer', 'mitgliedsnummern', 'mitgliedernr'],
        'givenName' => ['givenname', 'firstname', 'forename', 'vorname'],
        'familyName' => ['familyname', 'lastname', 'surname', 'nachname', 'familienname', 'name'],
        'email' => ['email', 'mail', 'emailaddress', 'emailadresse'],
        'phone' => ['phone', 'phonenumber', 'telephone', 'tel', 'mobile', 'telefon', 'telefonnummer', 'mobil', 'handy', 'mobilnummer'],
        'birthDate' => ['birthdate', 'dateofbirth', 'birthday', 'dob', 'geburtsdatum', 'geburtstag', 'gebdatum'],
        'notes' => ['notes', 'note', 'comment', 'comments', 'remarks', 'bemerkung', 'bemerkungen', 'notiz', 'notizen',
            'kommentar', 'anmerkung', 'anmerkungen'],
    ];

    private const array GUARDIAN_WORDS = [
        'erziehungsberechtigter' => 0, 'erziehungsberechtigte' => 0, 'elternteil' => 0,
        'guardian' => 0, 'parent' => 0, 'eltern' => 0,
        'mother' => 1, 'mutter' => 1, 'father' => 2, 'vater' => 2,
    ];

    /**
     * @param list<string> $headers
     * @return array<string,string>
     */
    public static function suggest(array $headers): array
    {
        $byName = [];
        foreach (self::SYNONYMS as $field => $synonyms) {
            foreach ($synonyms as $synonym) {
                $byName[$synonym] = $field;
            }
        }
        $suggestions = [];
        foreach ($headers as $header) {
            $clean = preg_replace('/[^\p{L}\p{Nd}]/u', '', mb_convert_case($header, MB_CASE_LOWER_SIMPLE, 'UTF-8')) ?? '';
            $field = $byName[$clean] ?? self::guardian($clean, $byName);
            if ($field !== null && !isset($suggestions[$field])) {
                $suggestions[$field] = $header;
            }
        }

        return $suggestions;
    }

    /** @param array<string,string> $byName */
    private static function guardian(string $header, array $byName): ?string
    {
        foreach (self::GUARDIAN_WORDS as $word => $slot) {
            $position = strpos($header, $word);
            if ($position === false) {
                continue;
            }
            $rest = substr_replace($header, '', $position, strlen($word));
            if ($slot === 0) {
                $slot = str_contains($rest, '1') ? 1 : (str_contains($rest, '2') ? 2 : 0);
            }
            $field = $byName[preg_replace('/\p{Nd}/u', '', $rest) ?? ''] ?? '';
            if ($slot !== 0 && in_array($field, ['givenName', 'familyName', 'email', 'phone'], true)) {
                return 'guardian' . $slot . ucfirst($field);
            }

            return null;
        }

        return null;
    }
}
