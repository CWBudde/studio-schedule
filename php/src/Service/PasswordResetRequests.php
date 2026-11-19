<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateInterval;
use DateTimeZone;
use LogicException;
use Psr\Clock\ClockInterface;
use StudioSchedule\Mail\Renderer;

final readonly class PasswordResetRequests
{
    /** @param (Closure():void)|null $wakeMail */
    public function __construct(
        private PasswordResetPersistence $store,
        private ClockInterface $clock,
        private Renderer $mail,
        private string $baseUrl,
        private ?Closure $wakeMail = null,
    ) {}

    /**
     * Issue a recovery link to an eligible account and silently ignore other addresses.
     */
    public function request(#[\SensitiveParameter] string $email, string $locale): void
    {
        if ($this->baseUrl === '') {
            throw new LogicException('Password reset mail requires a configured application URL.');
        }

        $email = SetupInput::trim($email);
        if ($email === '' || str_contains($email, "\0")) {
            return;
        }

        $token = rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
        $queuedMail = $this->store->transactional(function () use ($email, $locale, $token): bool {
            $account = $this->store->lockResetRecipient($email);
            if ($account === null) {
                return false;
            }

            $data = $this->store->passwordChangedData($account['id'], $locale);
            $rendered = $this->mail->render($data['locale'], 'reset-password', [
                'SchoolName' => $data['schoolName'],
                'GivenName' => $data['givenName'],
                'Link' => $this->baseUrl . '/reset-password?token=' . rawurlencode($token),
                'ExpiresHours' => 1,
            ]);
            $now = $this->clock->now()->setTimezone(new DateTimeZone('UTC'));
            $this->store->replaceResetLink(
                $account['id'],
                $account['email'],
                hash('sha256', $token),
                $now,
                $now->add(new DateInterval('PT1H')),
                $rendered,
            );

            return true;
        });
        if ($queuedMail) {
            $this->wakeMail?->__invoke();
        }
    }
}
