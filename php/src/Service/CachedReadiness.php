<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use Doctrine\DBAL\Exception;
use Psr\Clock\ClockInterface;
use Psr\Log\LoggerInterface;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\ReadinessCache;
use StudioSchedule\Store\Schema;

final readonly class CachedReadiness implements Readiness
{
    private const int CACHE_SECONDS = 2;

    /** @param list<int> $expectedVersions */
    public function __construct(
        #[\SensitiveParameter]
        private string $databaseURL,
        private array $expectedVersions,
        private ClockInterface $clock,
        private LoggerInterface $logger,
    ) {}

    public function ready(): bool
    {
        if ($this->databaseURL === '' || $this->expectedVersions === []) {
            return false;
        }

        $versionHash = hash('sha256', implode(',', $this->expectedVersions));
        if (function_exists('apcu_enabled') && apcu_enabled()) {
            $key = 'studio-schedule:readyz:' . hash('sha256', $this->databaseURL . ':' . $versionHash);
            $cached = apcu_fetch($key);
            $now = $this->clock->now();
            if (is_array($cached) && ($cached['checked'] ?? null) instanceof DateTimeImmutable && is_bool($cached['ready'] ?? null)
                && $cached['checked'] <= $now && $now < $cached['checked']->modify('+' . self::CACHE_SECONDS . ' seconds')) {
                return $cached['ready'];
            }

            $ready = $this->check(null);
            apcu_store($key, ['checked' => $this->clock->now(), 'ready' => $ready], self::CACHE_SECONDS);

            return $ready;
        }

        return $this->check($versionHash);
    }

    private function check(?string $sharedVersionHash): bool
    {
        $connection = Database::connect($this->databaseURL, dedicated: true, timeoutSeconds: 2);
        try {
            Database::limitMonitoringLockWait($connection);
            $probe = new DatabaseReadiness(new Schema($connection), $this->expectedVersions, $this->logger);
            if ($sharedVersionHash === null) {
                return $probe->ready();
            }

            return (new ReadinessCache($connection, $this->clock, self::CACHE_SECONDS))->remember($sharedVersionHash, $probe->ready(...));
        } catch (Exception $error) {
            $this->logger->debug('could not check database readiness', ['exception' => $error::class]);

            return false;
        } finally {
            $connection->close();
        }
    }
}
