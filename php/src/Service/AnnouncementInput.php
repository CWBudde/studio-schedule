<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use StudioSchedule\Mail\Formatting;
use StudioSchedule\Domain\AnnouncementAudience;
use StudioSchedule\Domain\Id;

final readonly class AnnouncementInput
{
    public function __construct(
        public AnnouncementAudience $audience,
        public string $title,
        public string $body,
        public ?DateTimeImmutable $publishAt,
        public ?DateTimeImmutable $expiresAt,
        public bool $email,
        public bool $draft,
    ) {}

    /** @param array<array-key,mixed> $fields */
    public static function audience(array $fields): AnnouncementAudience
    {
        return self::parseAudience(self::canonicalFields($fields, []));
    }

    /** @param array<string,mixed> $fields */
    private static function parseAudience(array $fields): AnnouncementAudience
    {
        $kind = $fields['audience'] ?? null;
        if (!is_string($kind) || !in_array($kind, ['everyone', 'role', ...array_keys(AnnouncementAudience::ID_FIELDS)], true)) {
            throw AnnouncementProblem::field('audience', 'invalid audience');
        }
        $role = $fields['role'] ?? null;
        if (($kind === 'role' && !in_array($role, ['member', 'instructor', 'office', 'admin'], true))
            || ($kind !== 'role' && $role !== null)) {
            throw AnnouncementProblem::field('role', 'a valid role is needed exactly for the role audience');
        }
        $reference = null;
        foreach (AnnouncementAudience::ID_FIELDS as $category => $field) {
            $value = $fields[$field] ?? null;
            if ($kind === $category) {
                if (!is_string($value) || !Id::valid($value)) {
                    throw AnnouncementProblem::field($field, 'expected a valid ID for this audience');
                }
                $reference = strtolower($value);
            } elseif ($value !== null) {
                throw AnnouncementProblem::field($field, 'this ID is only allowed for its audience');
            }
        }

        return new AnnouncementAudience($kind, is_string($role) ? $role : null, $reference);
    }

    /** @param array<array-key,mixed> $fields */
    public static function fromFields(array $fields): self
    {
        $fields = self::canonicalFields($fields, ['title', 'body', 'publishAt', 'expiresAt', 'email', 'draft']);
        $audience = self::parseAudience($fields);
        $title = self::text($fields['title'] ?? null, 'title', 120);
        if (str_contains($title, "\n")) {
            throw AnnouncementProblem::field('title', 'title must be one line');
        }
        $body = self::text($fields['body'] ?? null, 'body', 4000);
        $email = $fields['email'] ?? false;
        $draft = $fields['draft'] ?? false;
        if (!is_bool($email) || !is_bool($draft)) {
            throw AnnouncementProblem::field(is_bool($email) ? 'draft' : 'email', 'expected a boolean');
        }

        return new self(
            $audience,
            $title,
            $body,
            self::instant($fields['publishAt'] ?? null, 'publishAt'),
            self::instant($fields['expiresAt'] ?? null, 'expiresAt'),
            $email,
            $draft,
        );
    }

    /**
     * @param array<array-key,mixed> $fields
     * @param list<string> $editable
     * @return array<string,mixed>
     */
    private static function canonicalFields(array $fields, array $editable): array
    {
        $names = ['audience', 'role', ...array_values(AnnouncementAudience::ID_FIELDS), ...$editable];
        $canonical = array_combine(array_map(strtolower(...), $names), $names);
        $out = [];
        foreach ($fields as $field => $value) {
            $key = $canonical[strtolower((string) $field)] ?? null;
            if ($key === null) {
                throw AnnouncementProblem::field((string) $field, 'unexpected property');
            }
            $out[$key] = $value;
        }

        return $out;
    }

    private static function text(mixed $value, string $field, int $limit): string
    {
        if (!is_string($value)) {
            throw AnnouncementProblem::field($field, 'expected a string');
        }
        $text = Formatting::trimWhitespace(str_replace(["\r\n", "\r", "\u{2028}", "\u{2029}"], "\n", $value));
        if ($text === '' || mb_strlen($text, 'UTF-8') > $limit) {
            throw AnnouncementProblem::field($field, "expected 1 to $limit characters");
        }
        if (preg_match('/(?![\n\t\x{200C}\x{200D}])[\p{Cc}\p{Cf}]/u', $text) !== 0) {
            throw AnnouncementProblem::field($field, 'must not contain control or invisible format characters other than line breaks and tabs');
        }

        return $text;
    }

    private static function instant(mixed $value, string $field): ?DateTimeImmutable
    {
        if ($value === null) {
            return null;
        }
        if (!is_string($value) || preg_match('/\A(\d{4})-(\d{2})-(\d{2})T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d+)?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)\z/D', $value, $parts) !== 1
            || !checkdate((int) $parts[2], (int) $parts[3], (int) $parts[1])) {
            throw AnnouncementProblem::field($field, 'expected an RFC 3339 time');
        }

        return new DateTimeImmutable($value);
    }
}
