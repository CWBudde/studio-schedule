<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateInterval;
use DateTimeZone;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Mail\Renderer;

final readonly class AccountInvitation
{
    private const int EXPIRY_HOURS = 72;

    /**
     * @param Closure():string $baseUrl
     * @param (Closure():void)|null $wakeMail
     */
    public function __construct(
        private AccountInvitationPersistence $store,
        private ClockInterface $clock,
        private Renderer $mail,
        private Closure $baseUrl,
        private ?Closure $wakeMail = null,
    ) {}

    /** @return array{id:string,email:string,role:string,status:string} */
    public function create(SessionPrincipal $principal, string $personId, #[SensitiveParameter] string $email, string $role): array
    {
        if ($role === 'admin' && $principal->role !== 'admin') {
            throw new CatalogFailure(403, 'forbidden');
        }
        if (!in_array($role, ['member', 'instructor', 'office', 'admin'], true)) {
            throw CatalogFailure::field('role', 'unknown account role');
        }
        $email = SetupInput::trim($email);
        if (!SetupInput::validEmail($email)) {
            throw CatalogFailure::field('email', 'invalid account email');
        }

        $token = rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
        $account = $this->store->inTransaction(function () use ($personId, $role, $email, $token): array {
            $id = $this->store->createPending($personId, $email, $role);
            $data = $this->store->mailData($id);
            $rendered = $this->mail->render($data['locale'], 'invite', [
                'SchoolName' => $data['schoolName'],
                'GivenName' => $data['givenName'],
                'Link' => ($this->baseUrl)() . '/reset-password?token=' . rawurlencode($token),
                'ExpiresHours' => self::EXPIRY_HOURS,
            ]);
            $now = $this->clock->now()->setTimezone(new DateTimeZone('UTC'));
            $expires = $now->add(new DateInterval('PT' . self::EXPIRY_HOURS . 'H'));
            $this->store->writeInvitation($id, $email, hash('sha256', $token), $now, $expires, $rendered);

            return ['id' => $id->toRfc4122(), 'email' => $email, 'role' => $role, 'status' => 'pending'];
        });
        $this->wakeMail?->__invoke();

        return $account;
    }
}
