<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

enum LoginFailure
{
    case InvalidCredentials;
    case EmailNotConfirmed;
}
