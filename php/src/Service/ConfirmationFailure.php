<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

enum ConfirmationFailure
{
    case InvalidToken;
    case InvalidCredentials;
    case AccountNotFound;
}
