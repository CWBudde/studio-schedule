<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use InvalidArgumentException;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Mail\DeliveryFailure;
use StudioSchedule\Mail\SmtpMailer;
use Symfony\Component\Mailer\Exception\UnexpectedResponseException;
use Symfony\Component\Mailer\Transport\TransportInterface;
use Symfony\Component\Mime\Address;
use Symfony\Component\Mime\Email;

final class SmtpMailerTest extends TestCase
{
    public function testOnlyTheDocumentedConfigurationIsAccepted(): void
    {
        self::assertNull(SmtpMailer::fromEnvironment(static fn(string $key): string => ''));

        $defaults = ['SMTP_HOST' => 'mailpit', 'SMTP_FROM' => 'Studio Example <schedule@example.test>'];
        foreach ([
            [['SMTP_HOST' => 'mailpit:1025'], 'SMTP_HOST'],
            [['SMTP_PORT' => '65536'], 'SMTP_PORT'],
            [['SMTP_TLS' => 'ssl'], 'SMTP_TLS'],
            [['SMTP_FROM' => 'Studio Example <schedule@example.test>, stranger@example.test'], 'SMTP_FROM'],
            [['SMTP_FROM' => "schedule@example.test\r\nCc: stranger@example.test"], 'SMTP_FROM'],
            [['SMTP_PASSWORD' => 'fixture-secret'], 'SMTP_USERNAME'],
            [['SMTP_TLS' => 'none', 'SMTP_USERNAME' => 'studio', 'SMTP_PASSWORD' => 'fixture-secret'], 'localhost'],
        ] as [$override, $field]) {
            try {
                $config = $override + $defaults;
                SmtpMailer::fromEnvironment(static fn(string $key): string => $config[$key] ?? '');
                self::fail('Expected invalid SMTP configuration to fail');
            } catch (InvalidArgumentException $error) {
                self::assertStringContainsString($field, $error->getMessage());
                self::assertStringNotContainsString('fixture-secret', $error->getMessage());
                self::assertNull($error->getPrevious());
            }
        }

        self::assertInstanceOf(SmtpMailer::class, SmtpMailer::fromEnvironment(static fn(string $key): string => $defaults[$key] ?? ''));
    }

    public function testMessageContainsBothBodiesReplyToAndPdf(): void
    {
        $transport = self::createMock(TransportInterface::class);
        $transport->expects(self::once())->method('send')->with(self::callback(static function (Email $message): bool {
            self::assertSame('schedule@example.test', $message->getFrom()[0]->getAddress());
            self::assertSame('Studio Example', $message->getFrom()[0]->getName());
            self::assertSame('member@example.test', $message->getTo()[0]->getAddress());
            self::assertSame('office@example.test', $message->getReplyTo()[0]->getAddress());
            self::assertSame('Résumé', $message->getSubject());
            self::assertSame('Plain message.', $message->getTextBody());
            self::assertSame('<p>HTML message.</p>', $message->getHtmlBody());
            [$attachment] = $message->getAttachments();
            self::assertSame('application/pdf', $attachment->getMediaType() . '/' . $attachment->getMediaSubtype());
            self::assertSame('receipt.pdf', $attachment->getFilename());
            self::assertSame('%PDF-fixture', $attachment->getBody());

            return true;
        }));
        $mailer = new SmtpMailer($transport, new Address('schedule@example.test', 'Studio Example'));
        $mailer->send('member@example.test', 'Résumé', 'Plain message.', '<p>HTML message.</p>', 'office@example.test', 'receipt.pdf', '%PDF-fixture');
    }

    public function testTemporaryAndPermanentErrorsKeepServerDetailsOutOfTheFailure(): void
    {
        foreach ([451 => false, 550 => true] as $status => $permanent) {
            $transport = self::createStub(TransportInterface::class);
            $transport->method('send')->willThrowException(new UnexpectedResponseException('SMTP text contains fixture-secret', $status));
            $mailer = new SmtpMailer($transport, new Address('schedule@example.test'));
            try {
                $mailer->send('member@example.test', 'Subject', 'Plain message.', '');
                self::fail('Expected the transport to reject the message');
            } catch (DeliveryFailure $failure) {
                self::assertSame($permanent, $failure->permanent);
                self::assertStringContainsString((string) $status, $failure->getMessage());
                self::assertStringNotContainsString('fixture-secret', $failure->getMessage());
                self::assertNull($failure->getPrevious());
            }
        }
    }

    public function testInvalidMessagesArePermanentWithoutConnecting(): void
    {
        $transport = self::createMock(TransportInterface::class);
        $transport->expects(self::never())->method('send');
        $mailer = new SmtpMailer($transport, new Address('schedule@example.test'));
        foreach ([
            ['Name <member@example.test>', 'Subject', 'Plain message.', null, null, null],
            ['member@example.test', "Subject\r\nCc: stranger@example.test", 'Plain message.', null, null, null],
            ['member@example.test', 'Subject', '', null, null, null],
            ['member@example.test', 'Subject', 'Plain message.', 'invalid-reply', null, null],
            ['member@example.test', 'Subject', 'Plain message.', null, '../receipt.pdf', '%PDF-fixture'],
            ['member@example.test', 'Subject', 'Plain message.', null, 'receipt.pdf', null],
        ] as [$to, $subject, $text, $replyTo, $name, $attachment]) {
            try {
                $mailer->send($to, $subject, $text, '', $replyTo, $name, $attachment);
                self::fail('Expected the invalid message to fail');
            } catch (DeliveryFailure $failure) {
                self::assertTrue($failure->permanent);
                self::assertNull($failure->getPrevious());
            }
        }
    }

    public function testStartTlsDoesNotSendMailWhenTheServerDoesNotOfferIt(): void
    {
        foreach (['plain', 'helo'] as $fixture) {
            [$commands, $failure] = self::exchange('starttls', $fixture);
            self::assertInstanceOf(DeliveryFailure::class, $failure);
            self::assertFalse($failure->permanent);
            self::assertSame($fixture === 'helo' ? ['EHLO', 'HELO'] : ['EHLO'], $commands);
        }
    }

    public function testPlaintextModeDoesNotUpgradeAndAuthenticatesOnlyWhenConfigured(): void
    {
        [$commands, $failure] = self::exchange('none', 'starttls');
        self::assertNull($failure);
        self::assertSame(['EHLO', 'MAIL', 'RCPT', 'DATA', 'QUIT'], $commands);

        [$commands, $failure] = self::exchange('none', 'auth', authenticate: true);
        self::assertNull($failure);
        self::assertSame(['EHLO', 'AUTH', 'MAIL', 'RCPT', 'DATA', 'QUIT'], $commands);

        [$commands, $failure] = self::exchange('none', 'plain', authenticate: true);
        self::assertInstanceOf(DeliveryFailure::class, $failure);
        self::assertSame(['EHLO'], $commands);
    }

    public function testHealthProbeAuthenticatesAndClosesWithoutSendingMail(): void
    {
        [$commands, $failure] = self::exchange('none', 'auth', authenticate: true, verify: true);
        self::assertNull($failure);
        self::assertSame(['EHLO', 'AUTH', 'QUIT'], $commands);

        [$commands, $failure] = self::exchange('none', 'plain', authenticate: true, verify: true);
        self::assertInstanceOf(DeliveryFailure::class, $failure);
        self::assertSame(['EHLO'], $commands);
    }

    /** @return array{list<string>, ?DeliveryFailure} */
    private static function exchange(string $mode, string $fixture, bool $authenticate = false, bool $verify = false): array
    {
        $server = <<<'PHP_SERVER'
            $listener = stream_socket_server('tcp://127.0.0.1:0');
            if ($listener === false) {
                exit(1);
            }
            fwrite(STDOUT, stream_socket_get_name($listener, false) . "\n");
            $client = stream_socket_accept($listener, 5);
            if ($client === false) {
                exit(1);
            }
            stream_set_timeout($client, 5);
            fwrite($client, "220 fixture SMTP\r\n");
            $commands = [];
            $data = false;
            while (($line = fgets($client)) !== false) {
                if ($data && trim($line) !== '.') {
                    continue;
                }
                $command = explode(' ', trim($line), 2)[0];
                if ($command !== '.') {
                    $commands[] = $command;
                }
                $data = $command === 'DATA';
                $reply = match ($command) {
                    'EHLO' => match ($argv[1]) {
                        'auth' => "250-fixture\r\n250 AUTH PLAIN\r\n",
                        'helo' => "500 use HELO\r\n",
                        'starttls' => "250-fixture\r\n250 STARTTLS\r\n",
                        default => "250 fixture\r\n",
                    },
                    'AUTH' => base64_decode(explode(' ', trim($line), 3)[2] ?? '', true) === "\x00" . '0' . "\x00fixture-secret"
                        ? "235 authenticated\r\n"
                        : "535 invalid credentials\r\n",
                    'DATA' => "354 enter message\r\n",
                    'QUIT' => "221 closing\r\n",
                    default => "250 OK\r\n",
                };
                fwrite($client, $reply);
                if ($command === 'QUIT') {
                    break;
                }
            }
            fclose($client);
            fclose($listener);
            fwrite(STDOUT, json_encode($commands));
            PHP_SERVER;
        $process = proc_open([PHP_BINARY, '-r', $server, '--', $fixture], [1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes);
        self::assertIsResource($process);

        try {
            stream_set_timeout($pipes[1], 6);
            $socket = fgets($pipes[1]);
            self::assertIsString($socket);
            $port = parse_url('tcp://' . trim($socket), PHP_URL_PORT);
            self::assertIsInt($port);
            $config = ['SMTP_HOST' => '127.0.0.1', 'SMTP_PORT' => (string) $port, 'SMTP_TLS' => $mode, 'SMTP_FROM' => 'schedule@example.test'];
            if ($authenticate) {
                $config += ['SMTP_USERNAME' => '0', 'SMTP_PASSWORD' => 'fixture-secret'];
            }
            $mailer = SmtpMailer::fromEnvironment(static fn(string $key): string => $config[$key] ?? '');
            self::assertNotNull($mailer);
            $failure = null;
            try {
                if ($verify) {
                    $mailer->verify();
                } else {
                    $mailer->send('member@example.test', 'Subject', 'Plain message.', '');
                }
            } catch (DeliveryFailure $error) {
                $failure = $error;
            }
            unset($mailer);

            $output = stream_get_contents($pipes[1]);
            self::assertIsString($output);
            $commands = json_decode($output, flags: JSON_THROW_ON_ERROR);
            self::assertIsArray($commands);
            self::assertTrue(array_is_list($commands));
            self::assertContainsOnlyString($commands);
            self::assertSame('', stream_get_contents($pipes[2]));

            return [$commands, $failure];
        } finally {
            foreach ($pipes as $pipe) {
                fclose($pipe);
            }
            proc_close($process);
        }
    }
}
