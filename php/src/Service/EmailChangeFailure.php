<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

enum EmailChangeFailure
{
    case WrongPassword;
    case SameEmail;
    case Impersonating;
    case NoSession;
    case InvalidToken;
    case EmailTaken;
    case InvalidEmail;
}
