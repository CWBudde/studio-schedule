<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Exception\RetryableException;
use RuntimeException;

final class RegistrationChanged extends RuntimeException implements RetryableException {}
