<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Platforms\PostgreSQLPlatform;

final readonly class JobLocks
{
    public function __construct(#[\SensitiveParameter] private string $databaseURL) {}

    /**
     * Hold the lock on a dedicated connection while the job uses its own
     * transactions. Closing that connection releases the lock on every exit.
     *
     * @param callable():void $job
     */
    public function runIfFree(string $name, callable $job): bool
    {
        $connection = Database::connect($this->databaseURL, dedicated: true);
        try {
            $sql = $connection->getDatabasePlatform() instanceof PostgreSQLPlatform
                ? 'SELECT pg_try_advisory_lock(20, hashtext(?))'
                : "SELECT GET_LOCK(SHA2(CONCAT('studio-schedule:', DATABASE(), ':', ?), 256), 0)";
            $locked = $connection->fetchOne($sql, [$name]);
            if (!in_array($locked, [true, 't', 1, '1'], true)) {
                return false;
            }

            $job();

            return true;
        } finally {
            $connection->close();
        }
    }
}
