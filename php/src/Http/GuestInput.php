<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use SensitiveParameter;
use stdClass;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\SetupInput;

final class GuestInput
{
    /** @return array{name:string,email:string} */
    public static function read(#[SensitiveParameter] mixed $input, bool $allowNul = false): array
    {
        if (!$input instanceof stdClass) {
            throw CatalogFailure::field('guest', 'expected object');
        }
        $fields = [];
        foreach (get_object_vars($input) as $key => $value) {
            $field = strtolower((string) $key);
            if (!in_array($field, ['name', 'email'], true)) {
                throw CatalogFailure::field("guest.$key", 'unexpected property');
            }
            $fields[$field] = $value;
        }
        try {
            return [
                'name' => SetupInput::trim(CatalogAdminRoutes::text($fields, 'name', 200, true, $allowNul)),
                'email' => SetupInput::trim(CatalogAdminRoutes::text($fields, 'email', 254, allowNul: $allowNul) ?? ''),
            ];
        } catch (CatalogFailure $failure) {
            $field = $failure->location === 'body' ? 'guest' : 'guest.' . substr($failure->location, strlen('body.'));
            throw CatalogFailure::field($field, $failure->getMessage());
        }
    }
}
