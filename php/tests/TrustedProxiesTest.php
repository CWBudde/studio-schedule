<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use InvalidArgumentException;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Http\TrustedProxies;

final class TrustedProxiesTest extends TestCase
{
    public function testIPv4AndIPv6RangesAndSingleAddresses(): void
    {
        $trusted = TrustedProxies::parse(' 192.0.2.2, 10.45.9.34/20, ,2001:db8:12ab::/49, ::ffff:172.16.0.0/112 ');
        foreach (['192.0.2.2', '::ffff:192.0.2.2', '10.45.15.255', '2001:db8:12ab:7fff::5', '172.16.9.0'] as $address) {
            self::assertTrue($trusted->contains($address));
        }
        foreach (['192.0.2.3', '10.45.16.0', '2001:db8:12ab:8000::5', '172.17.9.0', 'not-an-address'] as $address) {
            self::assertFalse($trusted->contains($address));
        }
        self::assertFalse(TrustedProxies::parse('')->contains('127.0.0.1'));
    }

    /** @return list<array{string}> */
    public static function invalid(): array
    {
        return [['proxy.local'], ['10.1.2.3/33'], ['10.1.2.3/negative'], ['::ffff:172.16.0.0/63']];
    }

    #[DataProvider('invalid')]
    public function testRejectsInvalidPrefix(string $value): void
    {
        $this->expectException(InvalidArgumentException::class);
        $this->expectExceptionMessage('TRUSTED_PROXIES');
        TrustedProxies::parse($value);
    }
}
