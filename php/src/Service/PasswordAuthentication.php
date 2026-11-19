<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateInterval;
use DateTimeImmutable;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Store\SessionStore;

final readonly class PasswordAuthentication
{
    // A public, synthetic hash. Its result is ignored and verification costs
    // the same as for a wrong password on an existing account.
    private const string DUMMY_HASH = '$argon2id$v=19$m=19456,t=2,p=1$Zml4ZWQtdGVzdC1zYWx0IQ$8KTamOodG3oPmJE3MOLbJ7a/OFFb2HZ7u7tkLRZq9Hg';

    public function __construct(
        private SessionStore $store,
        private PasswordHash $passwords,
        private ClockInterface $clock,
    ) {}

    /** @return array{principal:SessionPrincipal,token:string,expiresAt:DateTimeImmutable}|LoginFailure */
    public function signIn(
        #[SensitiveParameter]
        string $email,
        #[SensitiveParameter]
        string $password,
    ): array|LoginFailure {
        // If a reset, account edit or competing rehash wins the write lock,
        // re-read and verify the current credentials before issuing a session.
        for ($attempt = 0; $attempt < 2; $attempt++) {
            $record = $this->store->credentialsForEmail($email);
            $verified = $this->passwords->verify($record['hash'] ?? self::DUMMY_HASH, $password);
            if ($record === null || !$verified || $record['disabled']) {
                return LoginFailure::InvalidCredentials;
            }
            if (!$record['confirmed']) {
                return LoginFailure::EmailNotConfirmed;
            }

            $replacement = $this->passwords->needsRehash($record['hash']) ? $this->passwords->hash($password) : null;
            $token = rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
            $now = $this->clock->now()->setTimezone(new DateTimeZone('UTC'));
            $expiresAt = $now->add(new DateInterval(SessionRecord::IDLE_TIMEOUT));
            $principal = $this->store->createForCredentials($record['id'], $email, $record['hash'], $replacement, hash('sha256', $token), $now, $expiresAt);
            if ($principal !== null) {
                return ['principal' => $principal, 'token' => $token, 'expiresAt' => $expiresAt];
            }
        }

        return LoginFailure::InvalidCredentials;
    }

    public function signOut(#[SensitiveParameter] string $token): void
    {
        if ($token !== '') {
            $this->store->end(hash('sha256', $token), $this->clock->now());
        }
    }
}
