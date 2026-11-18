<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use DateTimeImmutable;
use DateTimeZone;
use StudioSchedule\Config;

final readonly class SessionCookies
{
    public function __construct(private Config $config) {}

    public function issue(#[\SensitiveParameter] string $token, DateTimeImmutable $expiresAt): string
    {
        $expires = $expiresAt->setTimezone(new DateTimeZone('UTC'))->format('D, d M Y H:i:s \G\M\T');

        return $this->config->cookieName() . '=' . rawurlencode($token)
            . '; Path=' . $this->config->cookiePath() . '; Expires=' . $expires . $this->attributes();
    }

    public function expire(string $name): string
    {
        return $name . '=; Path=' . $this->config->cookiePath()
            . '; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Max-Age=0' . $this->attributes();
    }

    private function attributes(): string
    {
        return '; HttpOnly' . ($this->config->secure ? '; Secure' : '') . '; SameSite=Lax';
    }
}
