<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use JsonException;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use stdClass;
use StudioSchedule\Service\CommonPasswords;
use StudioSchedule\Service\PasswordReset;
use StudioSchedule\Service\PasswordResetResult;

final readonly class PasswordResetEndpoint
{
    private const int MAX_BODY_BYTES = 1024 * 1024;
    private const array FIELD_LIMITS = ['token' => [1, 100], 'password' => [CommonPasswords::MIN_LENGTH, 256]];

    public function __construct(private PasswordReset $reset) {}

    public function __invoke(#[\SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $stream = $request->getBody();
        $body = '';
        while (!$stream->eof() && strlen($body) < self::MAX_BODY_BYTES) {
            $chunk = $stream->read(self::MAX_BODY_BYTES - strlen($body));
            if ($chunk === '') {
                break;
            }
            $body .= $chunk;
        }
        if (strlen($body) >= self::MAX_BODY_BYTES) {
            return Responses::problem($request, 413, 'request body is too large limit=' . self::MAX_BODY_BYTES . ' bytes');
        }
        if ($body === '') {
            return Responses::problem($request, 400, 'request body is required');
        }
        $mediaType = strtolower(trim(explode(';', $request->getHeaderLine('Content-Type'), 2)[0]));
        if ($mediaType !== '' && $mediaType !== 'application/json' && !str_ends_with($mediaType, '+json')) {
            return self::invalid($request, $response, [['message' => 'unsupported content type', 'location' => 'body']], 415);
        }

        try {
            $input = json_decode($body, flags: JSON_THROW_ON_ERROR);
        } catch (JsonException) {
            return self::invalid($request, $response, [['message' => 'invalid JSON', 'location' => 'body']], 400);
        }
        if (!$input instanceof stdClass) {
            return self::invalid($request, $response, [['message' => 'expected object', 'location' => 'body']]);
        }

        $fields = array_change_key_case(get_object_vars($input));
        $errors = self::validateFields($fields);
        if ($errors !== []) {
            return self::invalid($request, $response, $errors);
        }

        assert(is_string($fields['token']) && is_string($fields['password']));

        return match ($this->reset->confirm($fields['token'], $fields['password'])) {
            PasswordResetResult::Completed => $response->withStatus(204),
            PasswordResetResult::InvalidToken => Responses::problem($request, 410, 'the link is invalid or has expired'),
            PasswordResetResult::CommonPassword => self::invalid($request, $response, [[
                'message' => 'the password is one of the most common ones; choose another', 'location' => 'body.password',
            ]]),
        };
    }

    /**
     * @param array<array-key, mixed> $fields
     * @return list<array{message: string, location: string}>
     */
    private static function validateFields(#[\SensitiveParameter] array $fields): array
    {
        $errors = [];
        foreach (self::FIELD_LIMITS as $field => [$min, $max]) {
            if (!array_key_exists($field, $fields)) {
                $errors[] = ['message' => "expected required property $field to be present", 'location' => 'body'];
                continue;
            }
            $value = $fields[$field];
            $message = self::fieldMessage($value, $min, $max);
            if ($message !== null) {
                $errors[] = ['message' => $message, 'location' => 'body.' . $field];
            }
        }
        if (array_diff_key($fields, self::FIELD_LIMITS) !== []) {
            $errors[] = ['message' => 'unexpected property', 'location' => 'body'];
        }

        return $errors;
    }

    private static function fieldMessage(#[\SensitiveParameter] mixed $value, int $min, int $max): ?string
    {
        if (!is_string($value)) {
            return 'expected string';
        }
        $length = mb_strlen($value, 'UTF-8');

        return match (true) {
            $length < $min => "expected length >= $min",
            $length > $max => "expected length <= $max",
            default => null,
        };
    }

    /** @param list<array{message: string, location: string}> $errors */
    private static function invalid(
        #[\SensitiveParameter]
        ServerRequestInterface $request,
        ResponseInterface $response,
        array $errors,
        int $status = 422,
    ): ResponseInterface {
        $response = $response->withStatus($status);
        $problem = ['title' => $response->getReasonPhrase(), 'status' => $status, 'detail' => 'validation failed', 'errors' => $errors];
        $requestId = $request->getAttribute('requestId');
        if (is_string($requestId)) {
            $problem['requestId'] = $requestId;
        }

        return Responses::json($response, $problem)
            ->withHeader('Content-Type', 'application/problem+json')
            ->withHeader('Cache-Control', 'no-store');
    }
}
