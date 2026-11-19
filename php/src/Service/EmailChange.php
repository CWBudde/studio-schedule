<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateInterval;
use DateTimeZone;
use LogicException;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Mail\Renderer;
use StudioSchedule\Store\EmailChangeStore;

final readonly class EmailChange
{
    private const int TOKEN_HOURS = 24;

    /** @param (Closure():void)|null $wakeMail */
    public function __construct(
        private EmailChangeStore $store,
        private PasswordHash $passwords,
        private ClockInterface $clock,
        private Renderer $mail,
        private string $baseUrl,
        private ?Closure $wakeMail = null,
    ) {}

    public function request(
        SessionPrincipal $principal,
        #[SensitiveParameter]
        string $presentedToken,
        #[SensitiveParameter]
        string $password,
        #[SensitiveParameter]
        string $newEmail,
        string $locale,
    ): ?EmailChangeFailure {
        if ($principal->impersonatorId !== null) {
            return EmailChangeFailure::Impersonating;
        }
        if ($presentedToken === '') {
            return EmailChangeFailure::NoSession;
        }
        if ($this->baseUrl === '') {
            throw new LogicException('Email change mail requires a configured application URL.');
        }

        $accountId = $principal->accountId;
        $newEmail = SetupInput::trim($newEmail);
        $hash = $this->store->passwordHash($accountId);
        $verifiedHash = $hash !== null && $this->passwords->verify($hash, $password) ? $hash : null;
        $sessionHash = hash('sha256', $presentedToken);
        $token = rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');

        $queuedMail = false;
        $failure = $this->store->transactional(function () use ($accountId, $newEmail, $locale, $verifiedHash, $sessionHash, $token, &$queuedMail): ?EmailChangeFailure {
            $queuedMail = false;
            $account = $this->store->lockEligibleAccount($accountId);
            if ($account === null) {
                return EmailChangeFailure::NoSession;
            }
            $sessionEnd = $this->store->lockSessionEnd($accountId, $sessionHash);
            $now = $this->clock->now();
            if ($sessionEnd === null || $now >= $sessionEnd) {
                return EmailChangeFailure::NoSession;
            }
            if (mb_convert_case($newEmail, MB_CASE_FOLD_SIMPLE, 'UTF-8') === mb_convert_case($account['email'], MB_CASE_FOLD_SIMPLE, 'UTF-8')) {
                return EmailChangeFailure::SameEmail;
            }
            if ($verifiedHash === null || !$this->store->matchesLockedPassword($accountId, $verifiedHash)) {
                return EmailChangeFailure::WrongPassword;
            }
            if (!SetupInput::validEmail($newEmail)) {
                return EmailChangeFailure::InvalidEmail;
            }
            if ($this->store->emailTaken($newEmail, $accountId)) {
                return null;
            }

            $data = $this->store->mailData($account['personId']);
            $rendered = $this->mail->render(self::mailLocale($account['locale'], $locale, $data['locale']), 'change-email', [
                'SchoolName' => $data['schoolName'],
                'GivenName' => $data['givenName'],
                'Link' => rtrim($this->baseUrl, '/') . '/confirm-email-change?token=' . rawurlencode($token),
                'ExpiresHours' => self::TOKEN_HOURS,
            ]);
            $expires = $now->setTimezone(new DateTimeZone('UTC'))->add(new DateInterval('PT' . self::TOKEN_HOURS . 'H'));
            $this->store->replaceLink($accountId, hash('sha256', $token), $newEmail, $now, $expires, $rendered);
            $queuedMail = true;
            return null;
        });
        if ($queuedMail) {
            $this->wakeMail?->__invoke();
        }

        return $failure;
    }

    public function confirm(#[SensitiveParameter] string $token, string $locale): ?EmailChangeFailure
    {
        $tokenHash = hash('sha256', $token);
        $accountId = $this->store->linkAccount($tokenHash);
        if ($accountId === null) {
            return EmailChangeFailure::InvalidToken;
        }

        $failure = $this->store->transactional(function () use ($accountId, $tokenHash, $locale): ?EmailChangeFailure {
            $account = $this->store->lockEligibleAccount($accountId);
            if ($account === null) {
                return EmailChangeFailure::InvalidToken;
            }
            $link = $this->store->lockLink($accountId, $tokenHash);
            $now = $this->clock->now();
            if ($link === null || $now >= $link['expiresAt']) {
                return EmailChangeFailure::InvalidToken;
            }
            $newEmail = $link['email'];
            if (!SetupInput::validEmail($newEmail)) {
                return EmailChangeFailure::InvalidEmail;
            }
            if ($this->store->emailTaken($newEmail, $accountId)) {
                return EmailChangeFailure::EmailTaken;
            }

            $data = $this->store->mailData($account['personId']);
            $rendered = $this->mail->render(self::mailLocale($account['locale'], $locale, $data['locale']), 'email-changed', [
                'SchoolName' => $data['schoolName'],
                'GivenName' => $data['givenName'],
                'NewEmail' => $newEmail,
            ]);
            $this->store->changeEmail($accountId, $account['email'], $newEmail, $now, $rendered);
            return null;
        });
        if ($failure === null) {
            $this->wakeMail?->__invoke();
        }

        return $failure;
    }

    private static function mailLocale(?string $account, string $requested, string $school): string
    {
        return $account !== null && $account !== '' ? $account : ($requested !== '' ? $requested : $school);
    }
}
