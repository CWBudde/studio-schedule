<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use JsonException;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use stdClass;
use StudioSchedule\Service\SetupInput;

final class JsonBody
{
    private const int MAX_BYTES = 1024 * 1024;

    public static function email(#[SensitiveParameter] string $value): ?string
    {
        // Go permits spaces and tabs around a bare address.
        $email = trim($value, " \t");

        return $email === SetupInput::trim($value) && SetupInput::validEmail($email) ? $email : null;
    }

    /**
     * @param array<string,array{int,int}> $limits
     * @param list<string> $optional
     * @return array<string,string>|ResponseInterface
     */
    public static function strings(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        array $limits,
        array $optional = [],
    ): array|ResponseInterface {
        $input = self::object($request, $response);
        if ($input instanceof ResponseInterface) {
            return $input;
        }

        $fields = array_change_key_case(get_object_vars($input));
        $values = [];
        $errors = [];
        foreach ($limits as $name => [$min, $max]) {
            $key = strtolower($name);
            $value = $fields[$key] ?? null;
            if ($value === null && in_array($name, $optional, true)) {
                continue;
            }
            if (!array_key_exists($key, $fields)) {
                $errors[] = ['location' => 'body', 'message' => "expected required property $name to be present"];
                continue;
            }

            $location = 'body.' . $name;
            if (!is_string($value)) {
                $errors[] = ['location' => $location, 'message' => 'expected string'];
                continue;
            }
            $length = mb_strlen($value, 'UTF-8');
            if ($length < $min || $length > $max) {
                $errors[] = [
                    'location' => $location,
                    'message' => $length < $min ? "expected length >= $min" : "expected length <= $max",
                ];
                continue;
            }
            $values[$name] = $value;
        }
        if (array_diff_key($fields, array_change_key_case($limits)) !== []) {
            $errors[] = ['location' => 'body', 'message' => 'unexpected property'];
        }

        return $errors === [] ? $values : Responses::validation($request, $response, $errors);
    }

    public static function object(
        #[SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
    ): stdClass|ResponseInterface {
        $body = '';
        $stream = $request->getBody();
        while (!$stream->eof() && strlen($body) <= self::MAX_BYTES) {
            $chunk = $stream->read(self::MAX_BYTES + 1 - strlen($body));
            if ($chunk === '') {
                break;
            }
            $body .= $chunk;
        }
        if (strlen($body) > self::MAX_BYTES) {
            return Responses::problem($request, 413, 'request body is too large limit=' . self::MAX_BYTES . ' bytes');
        }
        if ($body === '') {
            return Responses::problem($request, 400, 'request body is required');
        }
        $contentType = strtolower(trim(explode(';', $request->getHeaderLine('Content-Type'), 2)[0]));
        if ($contentType !== '' && $contentType !== 'application/json' && !str_ends_with($contentType, '+json')) {
            return Responses::problem($request, 415, 'unsupported content type');
        }

        try {
            $input = json_decode($body, flags: JSON_THROW_ON_ERROR);
        } catch (JsonException) {
            return Responses::problem($request, 400, 'invalid JSON');
        }

        return $input instanceof stdClass
            ? $input
            : Responses::validation($request, $response, [['location' => 'body', 'message' => 'expected object']]);
    }
}
