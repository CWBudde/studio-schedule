<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\ForeignKeyConstraintViolationException;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use SensitiveParameter;
use StudioSchedule\Mail\Rendered;
use StudioSchedule\Service\AccountInvitationPersistence;
use StudioSchedule\Service\CatalogFailure;
use Symfony\Component\Uid\Uuid;

final readonly class AccountInvitationStore implements AccountInvitationPersistence
{
    private FamilyStore $people;
    private PasswordResetStore $resets;

    public function __construct(private Connection $connection)
    {
        $this->people = new FamilyStore($connection);
        $this->resets = new PasswordResetStore($connection);
    }

    /**
     * @template T
     * @param Closure():T $write
     * @return T
     */
    public function inTransaction(Closure $write): mixed
    {
        return $this->people->inOfficeTransaction($write);
    }

    public function createPending(string $personId, #[SensitiveParameter] string $email, string $role): Uuid
    {
        // Creating an account makes the person managed. Serialize with family edits.
        if (!$this->people->lockLivePersons($personId)) {
            throw new CatalogFailure(404, 'resource not found');
        }
        $personKey = $this->key(Uuid::fromString($personId));
        if ($this->connection->fetchOne('SELECT 1 FROM accounts WHERE person_id = ?', [$personKey]) !== false) {
            throw new CatalogFailure(409, 'the person has an account already', reason: 'has-account');
        }

        $id = Uuid::v7();
        try {
            $this->connection->executeStatement(
                'INSERT INTO accounts (id, person_id, email, role) VALUES (?, ?, ?, ?)',
                [$this->key($id), $personKey, $email, $role],
            );
        } catch (UniqueConstraintViolationException) {
            // The unique email index also catches a concurrent registration or invitation.
            throw new CatalogFailure(409, 'the email address belongs to another account', reason: 'duplicate');
        } catch (ForeignKeyConstraintViolationException) {
            throw new CatalogFailure(404, 'resource not found');
        }

        return $id;
    }

    /** @return array{locale:string,schoolName:string,givenName:string} */
    public function mailData(Uuid $account): array
    {
        return $this->resets->passwordChangedData($this->key($account));
    }

    public function writeInvitation(
        Uuid $account,
        #[SensitiveParameter]
        string $email,
        #[SensitiveParameter]
        string $hash,
        DateTimeImmutable $now,
        DateTimeImmutable $expires,
        #[SensitiveParameter]
        Rendered $mail,
    ): void {
        $this->resets->replaceResetLink($this->key($account), $email, $hash, $now, $expires, $mail, template: 'invite');
    }

    private function key(Uuid $id): string
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform ? $id->toRfc4122() : $id->toBinary();
    }
}
