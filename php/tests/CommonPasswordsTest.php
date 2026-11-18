<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use PHPUnit\Framework\TestCase;
use StudioSchedule\Service\CommonPasswords;

final class CommonPasswordsTest extends TestCase
{
    public function testUsesTheGoPasswordListAndMatchesItsCaseFolding(): void
    {
        $path = getenv('STUDIO_PHP_TEST_COMMON_PASSWORDS') ?: dirname(__DIR__, 2) . '/internal/auth/password/common-passwords.txt';
        $passwords = new CommonPasswords($path);
        self::assertTrue($passwords->isCommon('PassWord'));
        self::assertTrue($passwords->isCommon('12345678'));
        self::assertTrue($passwords->isCommon('BASKETBALL'));
        self::assertTrue($passwords->isCommon('İLOVEYOU'));
        self::assertFalse($passwords->isCommon('123456'));
        self::assertFalse($passwords->isCommon('a rare phrase of words'));
        self::assertFalse($passwords->isCommon('password '));
    }

    public function testFailsWhenTheSharedListIsMissing(): void
    {
        $passwords = new CommonPasswords(__DIR__ . '/missing-password-list.txt');

        $this->expectException(\RuntimeException::class);
        $passwords->isCommon('password');
    }
}
