<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Store\SessionStore;

final readonly class Impersonation
{
    public function __construct(private SessionStore $store, private ClockInterface $clock) {}

    /**
     * @return array{principal:SessionPrincipal,token:string,expiresAt:DateTimeImmutable}|ImpersonationFailure
     * @phpstan-impure
     */
    public function start(SessionPrincipal $principal, #[SensitiveParameter] string $presentedToken, string $targetId): array|ImpersonationFailure
    {
        if ($principal->role !== 'admin' || $principal->impersonatorId !== null) {
            return ImpersonationFailure::NotImpersonable;
        }
        if ($targetId === '00000000-0000-0000-0000-000000000000'
            || preg_match('/\A[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\z/i', $targetId) !== 1) {
            return ImpersonationFailure::AccountNotFound;
        }
        $targetId = strtolower($targetId);
        if ($principal->accountId === $targetId) {
            return ImpersonationFailure::NotImpersonable;
        }

        return $this->rotate($principal->accountId, $targetId, true, $presentedToken);
    }

    /**
     * @return array{principal:SessionPrincipal,token:string,expiresAt:DateTimeImmutable}|ImpersonationFailure
     * @phpstan-impure
     */
    public function stop(SessionPrincipal $principal, #[SensitiveParameter] string $presentedToken): array|ImpersonationFailure
    {
        if ($principal->impersonatorId === null) {
            return ImpersonationFailure::NotImpersonating;
        }

        return $this->rotate($principal->impersonatorId, $principal->accountId, false, $presentedToken);
    }

    /** @return array{principal:SessionPrincipal,token:string,expiresAt:DateTimeImmutable}|ImpersonationFailure */
    private function rotate(string $actorId, string $targetId, bool $starting, #[SensitiveParameter] string $presentedToken): array|ImpersonationFailure
    {
        $token = rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
        $session = $this->store->rotateImpersonation(
            $actorId,
            $targetId,
            $starting,
            hash('sha256', $presentedToken),
            hash('sha256', $token),
            $this->clock,
        );
        if ($session instanceof ImpersonationFailure) {
            return $session;
        }

        return ['principal' => $session->principal, 'token' => $token, 'expiresAt' => $session->expiresAt];
    }
}
