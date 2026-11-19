<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

enum PasswordChangeFailure
{
    case CommonPassword;
    case WrongPassword;
    case Impersonating;
    case NoSession;
}
