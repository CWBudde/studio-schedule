<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use InvalidArgumentException;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use StudioSchedule\Service\MemberCsv;
use StudioSchedule\Service\MemberImportHeaders;

final class MemberImportEndpoint
{
    private const int MAX_ENCODED_BYTES = 1_398_104;
    private const int MAX_BODY_BYTES = self::MAX_ENCODED_BYTES + 65_536;

    public function __invoke(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $upload = self::readUpload($request);
        if ($upload instanceof ResponseInterface) {
            return $upload;
        }
        $csv = $upload['csv'];

        return Responses::json($response, [
            'encoding' => $csv->encoding,
            'delimiter' => match ($csv->delimiter) {
                ',' => 'comma',
                "\t" => 'tab',
                default => 'semicolon',
            },
            'headers' => $csv->headers,
            'rows' => count($csv->rows),
            'sample' => array_column(array_slice($csv->rows, 0, 5), 'cells'),
            'suggested' => ['columns' => (object) MemberImportHeaders::suggest($csv->headers)],
        ]);
    }

    /**
     * @param list<string> $allowed
     * @return array{name:string,csv:MemberCsv,fields:array<array-key,mixed>}|ResponseInterface
     */
    public static function readUpload(#[SensitiveParameter] ServerRequestInterface $request, array $allowed = []): array|ResponseInterface
    {
        $body = JsonObject::read($request, self::MAX_BODY_BYTES);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $fields = array_change_key_case(get_object_vars($body));
        foreach (array_keys($fields) as $name) {
            if (!in_array($name, ['filename', 'content', ...$allowed], true)) {
                return self::invalid($request, (string) $name, 'unexpected property');
            }
        }

        $name = $fields['filename'] ?? null;
        if (!is_string($name) || $name === '' || mb_strlen($name, 'UTF-8') > 255) {
            return self::invalid($request, 'fileName', 'expected a file name of 1 to 255 characters');
        }

        $encoded = $fields['content'] ?? null;
        if (!is_string($encoded) || strlen($encoded) > self::MAX_ENCODED_BYTES) {
            return self::invalid($request, 'content', 'expected a base64 encoded file of at most 1 MiB');
        }
        $bytes = base64_decode($encoded, true);
        if ($bytes === false || strlen($encoded) % 4 !== 0 || strpbrk($encoded, " \t\v\f\r\n") !== false) {
            return self::invalid($request, 'content', 'expected base64 encoded content');
        }

        try {
            $csv = MemberCsv::parse($bytes);
        } catch (InvalidArgumentException $error) {
            return self::invalid($request, 'content', $error->getMessage());
        }

        return ['name' => $name, 'csv' => $csv, 'fields' => $fields];
    }

    public static function invalid(ServerRequestInterface $request, string $field, string $message): ResponseInterface
    {
        return Responses::problem($request, 422, 'validation failed', errors: [['location' => 'body.' . $field, 'message' => $message]]);
    }
}
