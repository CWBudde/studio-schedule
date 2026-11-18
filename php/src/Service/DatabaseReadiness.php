<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Doctrine\DBAL\Exception;
use Psr\Log\LoggerInterface;
use StudioSchedule\Store\Schema;

final readonly class DatabaseReadiness implements Readiness
{
    /** @param list<int> $expectedVersions */
    public function __construct(
        private ?Schema $schema,
        private array $expectedVersions,
        private LoggerInterface $logger,
    ) {}

    public function ready(): bool
    {
        if ($this->schema === null || $this->expectedVersions === []) {
            return false;
        }

        try {
            return $this->schema->appliedVersions() === $this->expectedVersions;
        } catch (Exception $error) {
            $this->logger->debug('database is not ready', ['exception' => $error::class]);

            return false;
        }
    }
}
