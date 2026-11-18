<?php

declare(strict_types=1);

namespace StudioSchedule\Push;

use Psr\Http\Client\ClientExceptionInterface;
use RuntimeException;

final class TransportError extends RuntimeException implements ClientExceptionInterface {}
