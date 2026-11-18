<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use DateTimeZone;
use InvalidArgumentException;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Mail\Renderer;

final class MailRendererTest extends TestCase
{
    /** @param array<string, mixed> $data */
    #[DataProvider('goldenCases')]
    public function testRendersTheGoGoldenFiles(string $locale, string $template, string $golden, array $data): void
    {
        $dir = self::goldenDirectory();
        $prefix = "$dir/$locale/$golden";
        $mail = (new Renderer())->render($locale, $template, $data);

        self::assertStringEqualsFile("$prefix.subject.golden", $mail->subject . "\n");
        self::assertStringEqualsFile("$prefix.txt.golden", $mail->text);
        self::assertStringEqualsFile("$prefix.html.golden", $mail->html);
    }

    public function testEveryGoMailGoldenIsCovered(): void
    {
        $dir = self::goldenDirectory();
        $expected = [];
        foreach (self::goldenCases() as [$locale, , $golden]) {
            foreach (['subject', 'txt', 'html'] as $part) {
                $expected[] = "$dir/$locale/$golden.$part.golden";
            }
        }

        $found = [];
        foreach (['subject', 'txt', 'html'] as $part) {
            $found = [...$found, ...(glob("$dir/*/*.$part.golden") ?: [])];
        }

        sort($expected);
        sort($found);
        self::assertSame($expected, $found);
    }

    /** @return iterable<string, array{string, string, string, array<string, mixed>}> */
    public static function goldenCases(): iterable
    {
        // The inputs match the named fixtures in internal/mail/*_test.go.
        $recipient = ['SchoolName' => 'Studio Example', 'GivenName' => 'Anna'];
        $family = $recipient + ['Participant' => 'Mia Example'];
        $slot = [
            'CourseType' => 'Imaginary Ballet',
            'StartsAt' => self::berlin('2026-10-27 18:00'),
            'EndsAt' => self::berlin('2026-10-27 19:15'),
            'Room' => 'Room One',
            'Location' => 'Studio Nowhere',
        ];
        $lesson = $recipient + ['Participants' => ['Anna Example', 'Mia Example']] + $slot;
        $canceledSlot = $slot + ['Late' => true];
        $laterSlot = array_replace($slot, [
            'StartsAt' => self::berlin('2026-11-03 18:00'),
            'EndsAt' => self::berlin('2026-11-03 19:15'),
        ]);
        $lastSlot = array_replace($slot, [
            'StartsAt' => self::berlin('2026-11-10 18:00'),
            'EndsAt' => self::berlin('2026-11-10 19:15'),
            'Room' => 'Room Two',
        ]);
        $receipt = [
            'SchoolName' => 'Studio Example',
            'GivenName' => 'Clara',
            'Number' => 'SD-2026-0007',
            'Amount' => '150,00 €',
            'Item' => '10-visit pass for Ella <Example>, valid 1 Oct 2026 – 29 Mar 2027',
        ];
        $event = $family + ['Title' => 'Imaginary Workshop'];
        $session = [
            'StartsAt' => self::berlin('2026-07-04 10:00'),
            'EndsAt' => self::berlin('2026-07-04 13:00'),
            'Room' => 'Studio 1',
            'Location' => 'Example Street 1',
        ];
        $variants = [
            'booking-confirmed-guest' => 'booking-confirmed',
            'booking-canceled-several' => 'booking-canceled',
            'waitlist-expired-left' => 'waitlist-expired',
            'receipt-cancellation' => 'receipt',
        ];

        $cases = [
            'confirm-email' => $recipient + [
                'Link' => 'https://schedule.example.org/confirm-email?token=abc123',
                'ExpiresHours' => 24,
            ],
            'reset-password' => $recipient + [
                'Link' => 'https://schedule.example.org/reset-password?token=def456',
                'ExpiresHours' => 1,
            ],
            'change-email' => $recipient + [
                'Link' => 'https://schedule.example.org/change-email?token=ghi789',
                'ExpiresHours' => 24,
            ],
            'email-changed' => $recipient + ['NewEmail' => 'anna.new@example.org'],
            'invite' => $recipient + [
                'Link' => 'https://schedule.example.org/reset-password?token=jkl012',
                'ExpiresHours' => 72,
            ],
            'account-deleted' => $recipient,
            'password-changed' => $recipient,
            'test' => ['Server' => 'mail.example.org'],
            'lesson-canceled' => $lesson + ['Note' => 'The instructor is ill.'],
            'lesson-restored' => $lesson,
            'lesson-changed' => array_replace($lesson, [
                'StartsAt' => self::berlin('2026-10-28 19:30'),
                'EndsAt' => self::berlin('2026-10-28 20:45'),
                'Room' => 'Room Two',
                'PreviousStartsAt' => $slot['StartsAt'],
                'PreviousEndsAt' => $slot['EndsAt'],
                'PreviousRoom' => $slot['Room'],
                'PreviousLocation' => $slot['Location'],
            ]),
            'lesson-notice' => $lesson + [
                'Sender' => 'Ivy Example',
                'Message' => "Please bring ballet shoes & a water bottle.\nWe start <on time>.\n\nSee you!",
            ],
            'booking-confirmed' => $family + ['Lessons' => [$slot]],
            'booking-confirmed-guest' => $family + ['Lessons' => [$slot], 'Guest' => true],
            'booking-canceled' => $family + ['Lessons' => [$canceledSlot]],
            'booking-canceled-several' => $family + ['Lessons' => [$canceledSlot, $laterSlot, $lastSlot]],
            'waitlist-invite' => $family + $slot + [
                'Link' => 'https://school.example.org/waitlist/accept?token=abc123',
                'ExpiresAt' => self::berlin('2026-10-26 21:30'),
            ],
            'waitlist-expired' => $family + $slot + ['StillWaiting' => true],
            'waitlist-expired-left' => $family + $slot,
            'receipt' => $receipt,
            'receipt-cancellation' => array_replace($receipt, [
                'Cancellation' => true,
                'Number' => 'SD-2026-0008',
                'Cancels' => 'SD-2026-0007',
            ]),
            'announcement' => [
                'SchoolName' => 'Studio Example',
                'GivenName' => 'Clara',
                'Participants' => ['Ella Example', 'Finn Example'],
                'Title' => 'Costume fitting <Saturday>',
                'Message' => "Please come at 10:00 & bring the shoes.\nThe fitting takes an hour.\n\nSee you!",
            ],
            'event-canceled' => $recipient + [
                'Participants' => ['Anna Example', 'Mia Example'],
                'Title' => 'Imaginary Intensive',
                'FirstDay' => self::berlin('2026-07-03 10:00'),
                'LastDay' => self::berlin('2026-07-05 10:00'),
                'Note' => 'The guest teacher is ill.',
                'Refunded' => true,
            ],
            'event-registered' => $event + [
                'Sessions' => [
                    $session,
                    array_replace($session, [
                        'StartsAt' => self::berlin('2026-07-05 10:00'),
                        'EndsAt' => self::berlin('2026-07-05 13:00'),
                    ]),
                ],
                'Fee' => '€45.00',
                'CancelUntil' => self::berlin('2026-07-01 23:59'),
            ],
            'event-registration-canceled' => $event + ['Refunded' => true],
            'event-invite' => $event + [
                'ExpiresAt' => self::berlin('2026-07-04 09:00'),
                'Link' => 'https://example.org/events/invite?token=test',
            ],
            'event-invite-expired' => $event + ['ExpiresAt' => self::berlin('2026-07-04 09:00')],
        ];

        foreach (['en', 'de'] as $locale) {
            foreach ($cases as $golden => $data) {
                yield "$locale/$golden" => [$locale, $variants[$golden] ?? $golden, $golden, $data];
            }
        }
    }

    public function testSelectsTheBaseLanguageAndFallsBackToEnglish(): void
    {
        $renderer = new Renderer();

        foreach (['de-AT', ' de_CH ', 'DE', "\u{00A0}de-de\u{00A0}"] as $locale) {
            self::assertSame('de', $renderer->resolveLocale($locale));
        }

        foreach (['en-GB', 'fr-CA', '', '../de'] as $locale) {
            self::assertSame('en', $renderer->resolveLocale($locale));
        }

        $mail = $renderer->render('de-AT', 'test', ['Server' => 'mail.example.org']);
        self::assertStringContainsString('lang="de"', $mail->html);
    }

    public function testEscapesHtmlAndUrlsAndCollapsesSubjectWhitespace(): void
    {
        $link = "https://schedule.example.org/confirm?t=\"><script>x</script>&x=é+#%2B";
        $mail = (new Renderer())->render('en', 'confirm-email', [
            'SchoolName' => "Studio\r\nBcc:\tOther\u{00A0} Address & <b>Co's</b>",
            'GivenName' => '<script>alert("x")</script>',
            'Link' => $link,
            'ExpiresHours' => 1,
        ]);

        self::assertStringContainsString("Studio Bcc: Other Address & <b>Co's</b>", $mail->subject);
        self::assertStringNotContainsString("\n", $mail->subject);
        self::assertStringContainsString('Hello <script>alert("x")</script>', $mail->text);
        self::assertStringContainsString($link, $mail->text);
        self::assertStringContainsString('1 hour.', $mail->text);
        self::assertStringContainsString('&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;', $mail->html);
        self::assertStringContainsString('&amp; &lt;b&gt;Co&#39;s&lt;/b&gt;', $mail->html);
        self::assertStringContainsString('href="https://schedule.example.org/confirm?t=%22%3e%3cscript%3ex%3c/script%3e&amp;x=%c3%a9&#43;#%2B"', $mail->html);
        self::assertStringNotContainsString('<script>', $mail->html);
    }

    public function testAnUnsafeHrefIsFilteredInAnEventInvite(): void
    {
        $mail = (new Renderer())->render('en', 'event-invite', [
            'SchoolName' => 'Studio Example',
            'Title' => 'Imaginary Workshop',
            'Participant' => 'Mia Example',
            'ExpiresAt' => self::berlin('2026-07-04 09:00'),
            'Link' => 'javascript:alert(1)',
        ]);

        self::assertStringContainsString('href="#ZgotmplZ"', $mail->html);
        self::assertStringNotContainsString('href="javascript:', $mail->html);
        self::assertStringStartsWith("Hello,\n", $mail->text);
    }

    public function testAnEventWithoutSessionsOmitsTheDates(): void
    {
        $mail = (new Renderer())->render('en', 'event-canceled', [
            'SchoolName' => 'Studio Example',
            'Title' => 'Imaginary Workshop',
            'Participants' => ['Anna Example', 'Mia Example'],
        ]);

        self::assertStringContainsString("Imaginary Workshop\nRegistered: Anna Example, Mia Example", $mail->text);
        self::assertStringContainsString('<strong>Imaginary Workshop</strong><br>Registered:', $mail->html);
    }

    public function testUnknownTemplateIsRefused(): void
    {
        $this->expectException(InvalidArgumentException::class);
        (new Renderer())->render('en', '../layout', []);
    }

    /** @param array<string, mixed> $changes */
    #[DataProvider('invalidLinkData')]
    public function testInvalidLinkDataIsRefused(array $changes): void
    {
        $this->expectException(InvalidArgumentException::class);
        (new Renderer())->render('en', 'reset-password', $changes + [
            'SchoolName' => 'Studio Example',
            'Link' => 'https://schedule.example.org/reset-password?token=def456',
            'ExpiresHours' => 1,
        ]);
    }

    /** @return iterable<string, array{array<string, mixed>}> */
    public static function invalidLinkData(): iterable
    {
        yield 'empty school' => [['SchoolName' => "\u{00A0}"]];
        yield 'relative link' => [['Link' => '/reset-password']];
        yield 'unsafe scheme' => [['Link' => 'javascript:alert(1)']];
        yield 'missing host' => [['Link' => 'https:///reset-password']];
        yield 'control in link' => [['Link' => "https://example.org/?t=bad\nvalue"]];
        yield 'nonpositive expiry' => [['ExpiresHours' => 0]];
    }

    /** @param array<string, mixed> $data */
    #[DataProvider('invalidScheduleData')]
    public function testInvalidScheduleDataIsRefused(string $template, array $data): void
    {
        $this->expectException(InvalidArgumentException::class);
        (new Renderer())->render('de', $template, $data + [
            'SchoolName' => 'Studio Example',
            'Title' => 'Imaginary Workshop',
            'CourseType' => 'Imaginary Ballet',
            'Room' => 'Room One',
            'Location' => 'Studio Nowhere',
            'Participant' => 'Mia Example',
            'Participants' => ['Mia Example'],
        ]);
    }

    /** @return iterable<string, array{string, array<string, mixed>}> */
    public static function invalidScheduleData(): iterable
    {
        $first = self::berlin('2026-10-25 18:00');
        $last = self::berlin('2026-10-26 19:00');

        yield 'lesson without participants' => ['lesson-restored', ['Participants' => []]];
        yield 'lesson ends before it starts' => ['lesson-restored', ['StartsAt' => $last, 'EndsAt' => $first]];
        yield 'booking without lessons' => ['booking-confirmed', ['Lessons' => []]];
        yield 'malformed scheduled session' => ['event-registered', ['Sessions' => ['Studio 1']]];
        yield 'event with first date only' => ['event-canceled', ['FirstDay' => $first]];
        yield 'event dates inverted' => ['event-canceled', ['FirstDay' => $last, 'LastDay' => $first]];
        yield 'invitation without expiry' => ['event-invite', ['Link' => 'https://example.org/events/invite?token=test']];
    }

    private static function goldenDirectory(): string
    {
        return getenv('STUDIO_PHP_TEST_MAIL_GOLDENS') ?: dirname(__DIR__, 2) . '/internal/mail/testdata';
    }

    private static function berlin(string $wallTime): DateTimeImmutable
    {
        return new DateTimeImmutable($wallTime, new DateTimeZone('Europe/Berlin'));
    }
}
