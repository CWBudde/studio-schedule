<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use InvalidArgumentException;

final readonly class TrustedProxies
{
    /** @param list<array{string,int}> $ranges */
    private function __construct(private array $ranges) {}

    public static function parse(string $value): self
    {
        $ranges = [];
        foreach (explode(',', $value) as $item) {
            $item = trim($item);
            if ($item === '') {
                continue;
            }

            $parts = explode('/', $item, 2);
            $address = inet_pton($parts[0]);
            if ($address === false) {
                throw new InvalidArgumentException('TRUSTED_PROXIES contains an invalid IP address');
            }
            $size = strlen($address) * 8;
            $bits = $parts[1] ?? (string) $size;
            if (!ctype_digit($bits) || (int) $bits > $size) {
                throw new InvalidArgumentException('TRUSTED_PROXIES contains an invalid prefix length');
            }
            $width = (int) $bits;
            $normalized = self::unmap($address);
            if ($address !== $normalized) {
                $width -= 96;
                if ($width < 0) {
                    throw new InvalidArgumentException('TRUSTED_PROXIES IPv4-mapped prefixes must be at least /96');
                }
            }
            $ranges[] = [$normalized, $width];
        }

        return new self($ranges);
    }

    public function contains(string $address): bool
    {
        $packed = inet_pton($address);
        if ($packed === false) {
            return false;
        }
        $packed = self::unmap($packed);

        foreach ($this->ranges as [$range, $width]) {
            $bytes = intdiv($width, 8);
            if (strlen($range) !== strlen($packed) || substr($range, 0, $bytes) !== substr($packed, 0, $bytes)) {
                continue;
            }

            $bits = $width % 8;
            if ($bits === 0) {
                return true;
            }
            $shift = 8 - $bits;
            if ((ord($range[$bytes]) >> $shift) === (ord($packed[$bytes]) >> $shift)) {
                return true;
            }
        }

        return false;
    }

    public function clientIP(string $remote, string $forwarded): string
    {
        $peer = self::normalized($remote);
        if ($peer === null && preg_match('/\A(?:\[([^\]]+)\]|([^:]+)):\d+\z/', $remote, $match) === 1) {
            $peer = self::normalized($match[1] === '' ? $match[2] : $match[1]);
        }
        if ($peer === null) {
            return 'invalid IP';
        }
        if (!$this->contains($peer)) {
            return $peer;
        }

        $last = $peer;
        foreach (array_reverse(explode(',', $forwarded)) as $entry) {
            $address = self::normalized(trim($entry));
            if ($address === null) {
                return $last;
            }
            if (!$this->contains($address)) {
                return $address;
            }
            $last = $address;
        }

        return $last;
    }

    private static function normalized(string $address): ?string
    {
        if (str_contains($address, ':')) {
            $address = explode('%', $address, 2)[0];
        }
        $packed = inet_pton($address);

        return $packed === false ? null : (inet_ntop(self::unmap($packed)) ?: null);
    }

    private static function unmap(string $packed): string
    {
        return strlen($packed) === 16 && str_starts_with($packed, "\0\0\0\0\0\0\0\0\0\0\xff\xff")
            ? substr($packed, 12)
            : $packed;
    }
}
