<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

enum JobResult: string
{
    case Ok = 'ok';
    case Failed = 'failed';
    case Skipped = 'skipped';
}
