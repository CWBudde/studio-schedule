<?php

declare(strict_types=1);

namespace StudioSchedule\Mail;

use Closure;
use InvalidArgumentException;
use SensitiveParameter;
use Symfony\Component\Mailer\Exception\InvalidArgumentException as TransportArgumentException;
use Symfony\Component\Mailer\Exception\TransportException;
use Symfony\Component\Mailer\Exception\TransportExceptionInterface;
use Symfony\Component\Mailer\Transport\Smtp\EsmtpTransport;
use Symfony\Component\Mailer\Transport\Smtp\Stream\SocketStream;
use Symfony\Component\Mailer\Transport\TransportInterface;
use Symfony\Component\Mime\Address;
use Symfony\Component\Mime\Email;
use Symfony\Component\Mime\Exception\ExceptionInterface as MimeException;

final readonly class SmtpMailer implements Mailer
{
    private const int MAX_SUBJECT_LENGTH = 200;
    private const int MAX_ATTACHMENT_BYTES = 5 * 1024 * 1024;
    private const int TIMEOUT_SECONDS = 30;

    public function __construct(private TransportInterface $transport, private Address $from) {}

    /** @param Closure(string): string $get */
    public static function fromEnvironment(Closure $get): ?self
    {
        $host = Formatting::trimWhitespace($get('SMTP_HOST'));
        if ($host === '') {
            return null;
        }

        $ipv6 = filter_var($host, FILTER_VALIDATE_IP, FILTER_FLAG_IPV6) !== false;
        if (!$ipv6 && filter_var($host, FILTER_VALIDATE_DOMAIN, FILTER_FLAG_HOSTNAME) === false) {
            throw new InvalidArgumentException('SMTP_HOST must be a bare host name or IP address.');
        }

        $mode = Formatting::trimWhitespace($get('SMTP_TLS'));
        $mode = $mode === '' ? 'starttls' : $mode;
        $defaultPort = match ($mode) {
            'starttls' => 587,
            'tls' => 465,
            'none' => 25,
            default => throw new InvalidArgumentException('SMTP_TLS must be starttls, tls or none.'),
        };
        $port = Formatting::trimWhitespace($get('SMTP_PORT'));
        if ($port !== '' && (!ctype_digit($port) || (int) $port < 1 || (int) $port > 65535)) {
            throw new InvalidArgumentException('SMTP_PORT must be between 1 and 65535.');
        }

        $username = Formatting::trimWhitespace($get('SMTP_USERNAME'));
        $password = Formatting::trimWhitespace($get('SMTP_PASSWORD'));
        if (($username === '') !== ($password === '')) {
            throw new InvalidArgumentException('SMTP_USERNAME and SMTP_PASSWORD must be set together.');
        }
        if ($username !== '' && $mode === 'none' && !in_array($host, ['localhost', '127.0.0.1', '::1'], true)) {
            throw new InvalidArgumentException('SMTP authentication without TLS is only allowed for localhost.');
        }

        $sender = Formatting::trimWhitespace($get('SMTP_FROM'));
        if ($sender === '' || strpbrk($sender, "\r\n") !== false || (str_contains($sender, '<') && !str_ends_with($sender, '>'))) {
            throw new InvalidArgumentException('SMTP_FROM must be a single email address.');
        }
        try {
            $from = Address::create($sender);
        } catch (MimeException) {
            throw new InvalidArgumentException('SMTP_FROM must be a single email address.');
        }

        $transport = self::transport($ipv6 ? '[' . $host . ']' : $host, $port === '' ? $defaultPort : (int) $port, $mode, $username, $password);

        return new self($transport, $from);
    }

    public function send(
        #[SensitiveParameter]
        string $to,
        #[SensitiveParameter]
        string $subject,
        #[SensitiveParameter]
        string $text,
        #[SensitiveParameter]
        string $html,
        #[SensitiveParameter]
        ?string $replyTo = null,
        #[SensitiveParameter]
        ?string $attachmentName = null,
        #[SensitiveParameter]
        ?string $attachmentBytes = null,
    ): void {
        if (Formatting::trimWhitespace($subject) === '' || strpbrk($subject, "\r\n") !== false
            || mb_strlen($subject, 'UTF-8') > self::MAX_SUBJECT_LENGTH || Formatting::trimWhitespace($text) === '') {
            throw new DeliveryFailure('The mail subject or text is invalid.', true);
        }
        if (!self::validAttachment($attachmentName, $attachmentBytes)) {
            throw new DeliveryFailure('The mail attachment is invalid.', true);
        }

        try {
            $message = (new Email())->from($this->from)->to(self::bareAddress($to))->subject($subject)->text($text);
            if ($html !== '') {
                $message->html($html);
            }
            if ($replyTo !== null && $replyTo !== '') {
                $message->replyTo(self::bareAddress($replyTo));
            }
            if ($attachmentName !== null && $attachmentBytes !== null) {
                $message->attach($attachmentBytes, $attachmentName, 'application/pdf');
            }

            $this->transport->send($message);
        } catch (TransportExceptionInterface $error) {
            $status = $error->getCode();
            if ($status >= 400 && $status < 600) {
                throw new DeliveryFailure("SMTP delivery was rejected ($status).", $status >= 500);
            }

            throw new DeliveryFailure('SMTP delivery failed.');
        } catch (MimeException|TransportArgumentException) {
            throw new DeliveryFailure('The mail message is invalid.', true);
        } finally {
            if ($this->transport instanceof EsmtpTransport) {
                $this->transport->stop();
            }
        }
    }

    /** @return array{transport:string} */
    public function __debugInfo(): array
    {
        return ['transport' => $this->transport::class];
    }

    private static function bareAddress(#[SensitiveParameter] string $value): Address
    {
        if (preg_match('/[()<>"\x00-\x20\x7F]/', $value) === 1) {
            throw new DeliveryFailure('The mail address is invalid.', true);
        }

        return new Address($value);
    }

    private static function validAttachment(#[SensitiveParameter] ?string $name, #[SensitiveParameter] ?string $bytes): bool
    {
        if ($name === null && $bytes === null) {
            return true;
        }

        return $name !== null && $bytes !== null && $bytes !== '' && strlen($bytes) <= self::MAX_ATTACHMENT_BYTES
            && preg_match('/\A.{1,100}\z/us', $name) === 1 && preg_match('/[\x00-\x1F\x7F]/', $name) === 0
            && strpbrk($name, "\"/\\") === false;
    }

    private static function transport(
        string $host,
        int $port,
        string $mode,
        #[SensitiveParameter]
        string $username,
        #[SensitiveParameter]
        string $password,
    ): EsmtpTransport {
        $stream = (new SocketStream())->setTimeout(self::TIMEOUT_SECONDS)->setStreamOptions(['ssl' => [
            'crypto_method' => STREAM_CRYPTO_METHOD_TLSv1_2_CLIENT | STREAM_CRYPTO_METHOD_TLSv1_3_CLIENT,
            'verify_peer' => true,
            'verify_peer_name' => true,
        ]]);
        $transport = new class ($host, $port, $mode === 'tls', $stream, $username, $password) extends EsmtpTransport {
            private bool $extendedGreeting = false;

            public function __construct(
                string $host,
                int $port,
                bool $tls,
                SocketStream $stream,
                #[SensitiveParameter]
                private readonly string $authUsername,
                #[SensitiveParameter]
                private readonly string $authPassword,
            ) {
                parent::__construct($host, $port, $tls, stream: $stream, authenticators: []);
            }

            /** @param list<int> $codes */
            public function executeCommand(#[SensitiveParameter] string $command, array $codes): string
            {
                $hello = str_starts_with($command, 'HELO ');
                if ($hello) {
                    $this->extendedGreeting = false;
                }
                $response = parent::executeCommand($command, $codes);
                if (str_starts_with($command, 'EHLO ')) {
                    $this->extendedGreeting = true;
                }
                if ($hello) {
                    // Symfony returns early on the fallback to HELO, before enforcing require_tls.
                    if (!$this->extendedGreeting && $this->isTlsRequired()) {
                        throw new TransportException('The SMTP server did not negotiate STARTTLS.');
                    }
                    if ($this->authUsername !== '') {
                        $this->authenticate();
                    }
                }

                return $response;
            }

            protected function serverSupportsSmtpUtf8(): bool
            {
                return $this->extendedGreeting && parent::serverSupportsSmtpUtf8();
            }

            private function authenticate(): void
            {
                $methods = $this->extendedGreeting ? ($this->getCapabilities()['AUTH'] ?? []) : [];
                if (!is_array($methods) || !in_array('PLAIN', $methods, true)) {
                    throw new TransportException('The SMTP server did not offer AUTH PLAIN.');
                }

                $credentials = base64_encode("\x00" . $this->authUsername . "\x00" . $this->authPassword);
                $this->executeCommand("AUTH PLAIN {$credentials}\r\n", [235]);
            }
        };

        return $transport->setAutoTls($mode === 'starttls')->setRequireTls($mode === 'starttls');
    }
}
