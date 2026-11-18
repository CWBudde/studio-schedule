<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Service\PasswordHash;

final class PasswordHashTest extends TestCase
{
    private const string PASSWORD = "fictional Azur 🌙\0clé";
    // Produced by Go's password.NewWithParams(password.DefaultParams(), ...) using the salt "fixed-test-salt!".
    private const string GO_HASH = '$argon2id$v=19$m=19456,t=2,p=1$Zml4ZWQtdGVzdC1zYWx0IQ$8KTamOodG3oPmJE3MOLbJ7a/OFFb2HZ7u7tkLRZq9Hg';

    public function testVerifiesAnExistingGoServerHash(): void
    {
        $passwords = new PasswordHash();
        self::assertTrue($passwords->verify(self::GO_HASH, self::PASSWORD));
        self::assertFalse($passwords->verify(self::GO_HASH, "fictional Azur 🌙"));
        self::assertFalse($passwords->verify(self::GO_HASH, ''));
    }

    public function testCreatesHashesWithGoServerParametersAndFreshSalts(): void
    {
        $passwords = new PasswordHash();
        $first = $passwords->hash(self::PASSWORD);
        $second = $passwords->hash(self::PASSWORD);
        $format = '~\A\$argon2id\$v=19\$m=19456,t=2,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}\z~';
        self::assertMatchesRegularExpression($format, $first);
        self::assertMatchesRegularExpression($format, $second);
        self::assertNotSame($first, $second);
        self::assertTrue($passwords->verify($first, self::PASSWORD));
        self::assertTrue($passwords->verify($second, self::PASSWORD));
        self::assertFalse($passwords->verify($second, 'wrong password'));
    }

    public function testRoundTripsAnEmptyByteSequenceWithoutWarnings(): void
    {
        $passwords = new PasswordHash();
        self::assertTrue($passwords->verify($passwords->hash(''), ''));
    }

    #[DataProvider('invalidHashes')]
    public function testRejectsInvalidOrOutOfPolicyHashes(string $hash): void
    {
        self::assertFalse((new PasswordHash())->verify($hash, self::PASSWORD));
    }

    /** @return iterable<string, array{string}> */
    public static function invalidHashes(): iterable
    {
        yield 'algorithm' => [str_replace('$argon2id$', '$argon2i$', self::GO_HASH)];
        yield 'version' => [str_replace('$v=19$', '$v=16$', self::GO_HASH)];
        yield 'parameter order' => [str_replace('m=19456,t=2', 't=2,m=19456', self::GO_HASH)];
        yield 'integer overflow' => [str_replace('m=19456', 'm=18446744073709551616', self::GO_HASH)];
        yield 'below minimum memory' => [str_replace('m=19456', 'm=7', self::GO_HASH)];
        yield 'zero passes' => [str_replace('t=2', 't=0', self::GO_HASH)];
        yield 'zero lanes' => [str_replace('p=1', 'p=0', self::GO_HASH)];
        yield 'padding' => [self::GO_HASH . '='];
        yield 'noncanonical salt encoding' => [str_replace('YWx0IQ$', 'YWx0IR$', self::GO_HASH)];
        yield 'noncanonical key encoding' => [substr(self::GO_HASH, 0, -1) . 'h'];
        yield 'short salt' => [str_replace('Zml4ZWQtdGVzdC1zYWx0IQ', 'c2FsdA', self::GO_HASH)];
        yield 'short key' => [str_replace('8KTamOodG3oPmJE3MOLbJ7a/OFFb2HZ7u7tkLRZq9Hg', 'a2V5', self::GO_HASH)];
        yield 'trailing newline' => [self::GO_HASH . "\n"];

        // These keys are correct for their advertised parameters and the test password.
        yield 'excess memory' => ['$argon2id$v=19$m=131076,t=1,p=1$Zml4ZWQtdGVzdC1zYWx0IQ$QCdx8vP2zHxrIX870nVOvdzs8d1EwK6aiWo3nnTjmy4'];
        yield 'excess passes' => ['$argon2id$v=19$m=8,t=11,p=1$Zml4ZWQtdGVzdC1zYWx0IQ$09cm9sdUY/M99YrquhVOWXNFdudDYdp1N/QmeT9SgNg'];
        yield 'excess lanes' => ['$argon2id$v=19$m=136,t=1,p=17$Zml4ZWQtdGVzdC1zYWx0IQ$nDu/iQntM3P7jKACrqBF1LNwdjFPtPyWK+nOsrPLxbg'];
        yield 'long salt' => ['$argon2id$v=19$m=8,t=1,p=1$WlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlpaWlo$UVHx1DU6swvySwpxhH8st2tDj6yUsKkGX6/V9VH6DEU'];
        yield 'long key' => ['$argon2id$v=19$m=8,t=1,p=1$Zml4ZWQtdGVzdC1zYWx0IQ$PunBCl/l6BZIaGNqEVFnLdFLxLs/MYnVvHqyagkLmCmo7dciPvlmFmbakI3dIgz3ncyLJ/HvvZbrhGsKYAglSPc'];
    }
}
