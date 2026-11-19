<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

enum ImpersonationFailure
{
    case NotImpersonable;
    case AccountInactive;
    case NotImpersonating;
    case NoSession;
    case AccountNotFound;
}
