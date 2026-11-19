<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use SensitiveParameter;
use StudioSchedule\Service\SetupInput;
use StudioSchedule\Store\RateLimitReservation;
use StudioSchedule\Store\RateLimitStore;

final readonly class PasswordFailureLimit
{
    private const string BUCKET = 'password-failures';
    private const int LIMIT = 5;
    private const int WINDOW_SECONDS = 900;

    public function __construct(private RateLimitStore $limits) {}

    public function reserve(#[SensitiveParameter] string $email): RateLimitReservation|int
    {
        $key = mb_strtolower(SetupInput::trim($email), 'UTF-8');

        return $this->limits->reserve(self::BUCKET, $key, self::LIMIT, self::WINDOW_SECONDS);
    }
}
