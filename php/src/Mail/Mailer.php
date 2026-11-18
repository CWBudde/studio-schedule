<?php

declare(strict_types=1);

namespace StudioSchedule\Mail;

use SensitiveParameter;

interface Mailer
{
    public function send(
        #[SensitiveParameter]
        string $to,
        #[SensitiveParameter]
        string $subject,
        #[SensitiveParameter]
        string $text,
        #[SensitiveParameter]
        string $html,
        #[SensitiveParameter]
        ?string $replyTo = null,
        #[SensitiveParameter]
        ?string $attachmentName = null,
        #[SensitiveParameter]
        ?string $attachmentBytes = null,
    ): void;
}
