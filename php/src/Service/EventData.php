<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use Normalizer;

final readonly class EventData
{
    private const int MAX_SLUG_LENGTH = 80;

    public function __construct(
        public string $slug,
        public string $title,
        public ?string $description,
        public string $courseTypeId,
        public int $capacity,
        public int $price,
        public int $vatRate,
        public ?DateTimeImmutable $signupOpensAt,
        public ?DateTimeImmutable $signupClosesAt,
        public ?DateTimeImmutable $cancelUntil,
    ) {}

    public function validate(): void
    {
        if ($this->slug !== '' && (strlen($this->slug) > self::MAX_SLUG_LENGTH
            || preg_match('/\A[a-z0-9]+(?:-[a-z0-9]+)*\z/', $this->slug) !== 1)) {
            throw CatalogFailure::field('slug', 'the slug must be lowercase letters and digits joined by single hyphens, at most 80 characters');
        }
        if ($this->title === '') {
            throw CatalogFailure::field('title', 'the title must not be empty');
        }
        if ($this->signupOpensAt !== null && $this->signupClosesAt !== null && $this->signupClosesAt <= $this->signupOpensAt) {
            throw CatalogFailure::field('signupClosesAt', 'the sign-up must close after it opens');
        }
    }

    public function derivedSlug(): string
    {
        $german = strtr(mb_strtolower($this->title, 'UTF-8'), ['ä' => 'ae', 'ö' => 'oe', 'ü' => 'ue', 'ß' => 'ss']);
        $normalized = Normalizer::normalize($german, Normalizer::FORM_D);
        $letters = preg_replace('/\p{Mn}+/u', '', is_string($normalized) ? $normalized : $german) ?? '';
        $slug = trim(preg_replace('/[^a-z0-9]+/', '-', $letters) ?? '', '-');

        return self::cutSlug($slug, self::MAX_SLUG_LENGTH);
    }

    public static function numberedSlug(string $base, int $number): string
    {
        $suffix = '-' . $number;

        return self::cutSlug($base, self::MAX_SLUG_LENGTH - strlen($suffix)) . $suffix;
    }

    private static function cutSlug(string $slug, int $length): string
    {
        if (strlen($slug) <= $length) {
            return $slug;
        }
        $cut = strrpos(substr($slug, 0, $length + 1), '-');

        return substr($slug, 0, $cut === false ? $length : $cut);
    }
}
