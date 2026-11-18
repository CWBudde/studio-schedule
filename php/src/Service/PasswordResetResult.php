<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

enum PasswordResetResult
{
    case Completed;
    case InvalidToken;
    case CommonPassword;
}
