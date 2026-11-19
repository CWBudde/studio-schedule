<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Store\SessionStore;

final readonly class AccountConfirmation
{
    public function __construct(
        private SessionStore $store,
        private PasswordHash $passwords,
        private ClockInterface $clock,
    ) {}

    /** @return array{principal:SessionPrincipal,token:string,expiresAt:DateTimeImmutable}|ConfirmationFailure */
    public function confirm(
        #[SensitiveParameter]
        string $token,
        #[SensitiveParameter]
        string $password,
    ): array|ConfirmationFailure {
        $tokenHash = hash('sha256', $token);
        $candidate = $this->store->confirmationCandidate($tokenHash, $this->clock->now());
        if ($candidate === null) {
            return ConfirmationFailure::InvalidToken;
        }
        $verifiedHash = $candidate['hash'];
        if ($verifiedHash === null) {
            return ConfirmationFailure::AccountNotFound;
        }
        if (!$this->passwords->verify($verifiedHash, $password) || $candidate['disabled']) {
            return ConfirmationFailure::InvalidCredentials;
        }

        $replacement = $this->passwords->needsRehash($verifiedHash) ? $this->passwords->hash($password) : null;
        $sessionToken = rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
        $result = $this->store->confirmForCredentials(
            $candidate['id'],
            $tokenHash,
            $verifiedHash,
            $replacement,
            hash('sha256', $sessionToken),
            $this->clock,
        );
        if ($result instanceof ConfirmationFailure) {
            return $result;
        }

        return ['principal' => $result->principal, 'token' => $sessionToken, 'expiresAt' => $result->expiresAt];
    }
}
