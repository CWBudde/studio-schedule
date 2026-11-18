<?php

declare(strict_types=1);

namespace StudioSchedule\Mail;

final readonly class Rendered
{
    public function __construct(public string $subject, public string $text, public string $html) {}
}
