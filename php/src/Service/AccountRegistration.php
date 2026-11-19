<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateInterval;
use DateTimeZone;
use LogicException;
use Psr\Clock\ClockInterface;
use StudioSchedule\Mail\Renderer;
use StudioSchedule\Store\RegistrationStore;

final readonly class AccountRegistration
{
    private const int MAX_PASSWORD_LENGTH = 256;
    private const int TOKEN_HOURS = 24;

    /** @param (Closure():void)|null $wakeMail */
    public function __construct(
        private RegistrationStore $store,
        private PasswordHash $passwords,
        private CommonPasswords $commonPasswords,
        private ClockInterface $clock,
        private Renderer $mail,
        private string $baseUrl,
        private ?Closure $wakeMail = null,
    ) {}

    public function register(
        string $givenName,
        string $familyName,
        #[\SensitiveParameter]
        string $email,
        #[\SensitiveParameter]
        string $password,
        string $locale,
    ): void {
        if ($this->baseUrl === '') {
            throw new LogicException('Registration mail requires a configured application URL.');
        }

        $this->checkPassword($password);
        $givenName = SetupInput::name($givenName, 'givenName', 'given name');
        $familyName = SetupInput::name($familyName, 'familyName', 'family name');
        $email = SetupInput::trim($email);
        if (!SetupInput::validEmail($email)) {
            throw new InvalidSetupInput('email', 'invalid account email');
        }

        // Keep the expensive hash outside the transaction, including on retries.
        $passwordHash = $this->passwords->hash($password);
        $token = rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
        $storedLocale = $this->mail->matchLocale($locale);

        $queuedMail = $this->store->transactional(function () use ($givenName, $familyName, $email, $locale, $storedLocale, $passwordHash, $token): bool {
            $now = $this->clock->now()->setTimezone(new DateTimeZone('UTC'));
            $account = $this->store->savePending($givenName, $familyName, $email, $storedLocale);
            if ($account === null) {
                return false;
            }

            $school = $this->store->school();
            $accountLocale = $account['locale'] ?? '';
            $mailLocale = $accountLocale !== '' ? $accountLocale : $locale;
            $rendered = $this->mail->render($mailLocale !== '' ? $mailLocale : $school['locale'], 'confirm-email', [
                'SchoolName' => $school['name'],
                'GivenName' => $givenName,
                'Link' => $this->baseUrl . '/confirm?token=' . rawurlencode($token),
                'ExpiresHours' => self::TOKEN_HOURS,
            ]);

            $this->store->writeConfirmation(
                $account['id'],
                $account['email'],
                $passwordHash,
                hash('sha256', $token),
                $now,
                $now->add(new DateInterval('PT' . self::TOKEN_HOURS . 'H')),
                $rendered,
            );

            return true;
        });
        if ($queuedMail) {
            $this->wakeMail?->__invoke();
        }
    }

    private function checkPassword(#[\SensitiveParameter] string $password): void
    {
        $length = mb_strlen($password, 'UTF-8');
        if ($length < CommonPasswords::MIN_LENGTH) {
            throw new InvalidSetupInput('password', 'the password must have at least ' . CommonPasswords::MIN_LENGTH . ' characters');
        }
        if ($length > self::MAX_PASSWORD_LENGTH) {
            throw new InvalidSetupInput('password', 'the password must have at most ' . self::MAX_PASSWORD_LENGTH . ' characters');
        }
        if ($this->commonPasswords->isCommon($password)) {
            throw new InvalidSetupInput('password', 'the password is one of the most common ones; choose another');
        }
    }
}
