<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

enum BrandingImage: string
{
    case Logo = 'logo';
    case Background = 'background';

    public static function digest(string $contentType, string $data): string
    {
        return hash('sha256', $contentType . "\n" . $data);
    }
}
