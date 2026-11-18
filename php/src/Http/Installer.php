<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Log\LoggerInterface;
use StudioSchedule\Config;
use StudioSchedule\Service\AlreadyInitialized;
use StudioSchedule\Service\InstallationEnvironment;
use StudioSchedule\Service\InvalidSetupInput;
use StudioSchedule\Service\PasswordHash;
use StudioSchedule\Service\SchoolSetup;
use StudioSchedule\Service\SetupInput;
use StudioSchedule\Service\SystemClock;
use StudioSchedule\Store\CopyError;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\DatabaseCopy;
use StudioSchedule\Store\JobLocks;
use StudioSchedule\Store\MigrationRecoveryRequired;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;
use StudioSchedule\Store\SetupStore;
use Throwable;

final readonly class Installer
{
    public function __construct(
        private Config $config,
        private string $migrations,
        private LoggerInterface $logger,
        private DatabaseCopy $databaseCopy,
    ) {}

    public function __invoke(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $parsed = $request->getParsedBody();
        $body = is_array($parsed) ? $parsed : [];
        $post = $request->getMethod() === 'POST';
        $token = self::field($post ? $body : $request->getQueryParams(), 'token');
        if ($token === '' || !hash_equals($this->config->setupToken, $token)) {
            return Responses::problem($request, 404, 'not found');
        }

        $path = $this->config->basePath . '/setup';
        if ($this->config->databaseURL === '') {
            return self::message($response, 'Set DATABASE_URL to connect the installer to the database.', 503);
        }
        $driver = Database::driverForURL($this->config->databaseURL);
        if ($driver === null) {
            return self::message($response, 'DATABASE_URL must name PostgreSQL, MySQL or MariaDB.', 503);
        }
        $problem = InstallationEnvironment::phpProblem($driver);
        if ($problem !== null) {
            return self::message($response, $problem, 503);
        }

        $connection = null;
        try {
            $connection = Database::connect($this->config->databaseURL);
            $problem = InstallationEnvironment::databaseProblem($driver, Database::serverVersion($connection, $driver));
        } catch (Throwable $error) {
            $connection?->close();
            $this->logError($error);

            return self::message($response, 'The database connection failed. Check DATABASE_URL and the database permissions.', 503);
        }

        try {
            if ($problem !== null) {
                return self::message($response, $problem, 503);
            }
            $store = new SetupStore($connection);
            $backupRequired = !($connection->getDatabasePlatform() instanceof PostgreSQLPlatform);
            if ($post) {
                return $this->submit($response, $connection, $store, $body, $backupRequired);
            }

            $status = $this->status($connection, $store);
            $content = match (true) {
                !$status['initialized'] => InstallerView::install($path, $token),
                $status['pending'] !== [] => InstallerView::upgrade($path, $token, $status['pending'], $backupRequired),
                default => InstallerView::copy($path, $token),
            };

            return InstallerView::page($response, $content);
        } catch (MigrationRecoveryRequired $error) {
            $this->logError($error);

            return self::message($response, $error->getMessage(), 503);
        } catch (InvalidSetupInput|CopyError $error) {
            return self::message($response, $error->getMessage(), 422);
        } catch (AlreadyInitialized) {
            return self::message($response, 'The school is set up already.', 409);
        } catch (Throwable $error) {
            $this->logError($error);

            return self::message($response, 'Setup could not continue. Check the release migrations, database configuration, and server log.', 503);
        } finally {
            $connection->close();
        }
    }

    /** @param array<array-key,mixed> $body */
    private function submit(
        ResponseInterface $response,
        Connection $connection,
        SetupStore $store,
        #[\SensitiveParameter]
        array $body,
        bool $backupRequired,
    ): ResponseInterface {
        $reply = $response;
        $jobs = new JobLocks($this->config->databaseURL);
        $locked = $jobs->runIfFree('web-installer', function () use ($connection, $store, $body, $response, $backupRequired, &$reply): void {
            $state = $this->status($connection, $store);
            $action = self::field($body, 'action');
            if ($action === 'install' && !$state['initialized']) {
                $reply = $this->install($response, $store, $body);
            } elseif ($action === 'upgrade' && $state['initialized'] && $state['pending'] !== []) {
                $reply = $this->upgrade($response, $store, $body, $backupRequired);
            } elseif ($action === 'copy' && $state['initialized'] && $state['pending'] === []) {
                $reply = $this->copy($response, $store, $body);
            } else {
                $reply = self::message($response, 'The installation state changed. Reload the setup page.', 409);
            }
        });

        return $locked ? $reply : self::message($response, 'Another setup request is running. Reload the setup page later.', 409);
    }

    /** @param array<array-key,mixed> $body */
    private function install(ResponseInterface $response, SetupStore $store, #[\SensitiveParameter] array $body): ResponseInterface
    {
        $input = new SetupInput(
            self::field($body, 'schoolName'),
            self::field($body, 'timezone'),
            self::field($body, 'locale'),
            self::field($body, 'currency'),
            self::field($body, 'email'),
            self::field($body, 'givenName'),
            self::field($body, 'familyName'),
        );
        $input = $input->validated();
        (new Migrator($this->config->databaseURL))->up($this->migrations);
        $setup = (new SchoolSetup($store, new SystemClock()))->init($input);
        $link = $setup->passwordLink($this->config->baseURL . '/');

        return InstallerView::page($response, InstallerView::setupLink($link, $setup->expiresAt->format('Y-m-d H:i T')), 201);
    }

    /** @param array<array-key,mixed> $body */
    private function upgrade(
        ResponseInterface $response,
        SetupStore $store,
        #[\SensitiveParameter]
        array $body,
        bool $backupRequired,
    ): ResponseInterface {
        if (!self::authenticated($store, $body)) {
            return self::message($response, 'Enter the email and password of an active administrator.', 403);
        }

        $backup = self::field($body, 'backupVerified') === 'yes';
        if ($backupRequired && !$backup) {
            return self::message($response, 'Confirm that you stopped writes and verified a full pre-upgrade backup before applying migrations.', 422);
        }

        (new Migrator($this->config->databaseURL))->up($this->migrations, backupVerified: $backup);

        return self::message($response, 'All migrations were applied.');
    }

    /** @param array<array-key,mixed> $body */
    private function copy(ResponseInterface $response, SetupStore $store, #[\SensitiveParameter] array $body): ResponseInterface
    {
        if (!self::authenticated($store, $body)) {
            return self::message($response, 'Enter the email and password of an active administrator.', 403);
        }

        $target = SetupInput::trim(self::field($body, 'targetDatabaseUrl'));
        if ($target === '') {
            return self::message($response, 'Enter the target database URL.', 422);
        }
        $rows = $this->databaseCopy->copy($this->config->databaseURL, $target);

        return self::message($response, "Copied $rows rows. Database integrity checks passed.");
    }

    /** @param array<array-key,mixed> $body */
    private static function authenticated(SetupStore $store, #[\SensitiveParameter] array $body): bool
    {
        $email = trim(self::field($body, 'email'));
        $password = self::field($body, 'password');
        if ($email === '' || $password === '' || strlen($password) > 1024) {
            return false;
        }
        $hash = $store->adminPasswordHash($email);

        return $hash !== null && (new PasswordHash())->verify($hash, $password);
    }

    /** @return array{initialized:bool,pending:list<string>} */
    private function status(Connection $connection, SetupStore $store): array
    {
        $state = (new Schema($connection))->migrationState(Schema::migrationFiles($this->migrations));

        return [
            'initialized' => $state['applied'] !== [] && $store->isInitialized(),
            'pending' => array_map(basename(...), array_values($state['pending'])),
        ];
    }

    private static function message(ResponseInterface $response, string $message, int $status = 200): ResponseInterface
    {
        return InstallerView::page($response, InstallerView::message($message), $status);
    }

    /** @param array<array-key,mixed> $values */
    private static function field(#[\SensitiveParameter] array $values, string $key): string
    {
        $value = $values[$key] ?? null;

        return is_string($value) ? $value : '';
    }

    private function logError(Throwable $error): void
    {
        $this->logger->error('installer failed', ['exception' => $error::class]);
    }
}
