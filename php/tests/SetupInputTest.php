<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Service\InvalidSetupInput;
use StudioSchedule\Service\SetupInput;

final class SetupInputTest extends TestCase
{
    #[DataProvider('invalidFields')]
    public function testRejectsInvalidInput(string $argument, string $value, string $field): void
    {
        $input = [
            'schoolName' => 'Example Studio',
            'timezone' => 'Europe/Berlin',
            'locale' => 'en',
            'currency' => 'EUR',
            'adminEmail' => 'admin@example.org',
            'adminGivenName' => 'Adele',
            'adminFamilyName' => 'Example',
        ];
        $input[$argument] = $value;

        try {
            (new SetupInput(...$input))->validated();
            self::fail('The invalid setup input was accepted');
        } catch (InvalidSetupInput $error) {
            self::assertSame($field, $error->field);
        }
    }

    /** @return iterable<string, array{string, string, string}> */
    public static function invalidFields(): iterable
    {
        yield 'blank school name' => ['schoolName', "\u{2003}", 'schoolName'];
        yield 'school name too long' => ['schoolName', str_repeat('Ä', 201), 'schoolName'];
        yield 'embedded NUL' => ['schoolName', "Studio\0name", 'schoolName'];
        yield 'malformed UTF-8' => ['adminFamilyName', "\xFF", 'familyName'];
        yield 'unnamed time zone' => ['timezone', '+03:00', 'timezone'];
        yield 'missing time zone' => ['timezone', '', 'timezone'];
        yield 'local time zone' => ['timezone', 'Local', 'timezone'];
        yield 'unknown time zone' => ['timezone', 'Nowhere/Town', 'timezone'];
        yield 'unsupported language' => ['locale', 'fr', 'defaultLocale'];
        yield 'unknown currency' => ['currency', 'ZZZ', 'currency'];
        yield 'long currency' => ['currency', 'EURO', 'currency'];
        yield 'truncated currency' => ['currency', 'UR', 'currency'];
        yield 'blank email' => ['adminEmail', ' ', 'email'];
        yield 'display-name email' => ['adminEmail', 'Ada <admin@example.org>', 'email'];
        yield 'invalid local part' => ['adminEmail', 'admin..office@example.org', 'email'];
        yield 'quoted local part' => ['adminEmail', '"office"@example.org', 'email'];
        yield 'unprefixed IPv6 literal' => ['adminEmail', 'Ada@[2001:db8::1]', 'email'];
        yield 'blank given name' => ['adminGivenName', '', 'givenName'];
        yield 'blank family name' => ['adminFamilyName', ' ', 'familyName'];
    }

    public function testPreservesGoBareEmailFormsAndNamesOfExactly200Characters(): void
    {
        $school = str_repeat('Ä', 200);
        foreach (['Éda@bureau', 'Ada@[127.0.0.1]', 'Ada@[IPv6:2001:db8::1]', 'Ada@[::ffff:127.0.0.1]'] as $email) {
            $input = (new SetupInput($school, 'US/Eastern', 'en-GB', ' uſd ', $email, 'Ada', 'Example'))->validated();
            self::assertSame($school, $input->schoolName);
            self::assertSame($email, $input->adminEmail);
            self::assertSame('en', $input->locale);
            self::assertSame('USD', $input->currency);
        }
    }
}
