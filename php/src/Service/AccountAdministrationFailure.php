<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

enum AccountAdministrationFailure
{
    case NotFound;
    case OwnAccount;
    case LastAdmin;
    case InUse;
    case Impersonating;
    case WrongPassword;
}
