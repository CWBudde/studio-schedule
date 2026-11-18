<?php

declare(strict_types=1);

namespace StudioSchedule\Console;

final class Healthcheck
{
    public static function check(string $target): void
    {
        $handle = curl_init($target);
        if ($handle === false) {
            throw new CommandError('health check: could not initialize the HTTP request');
        }

        $response = '';
        $finished = false;
        curl_setopt_array($handle, [
            CURLOPT_PROXY => '',
            CURLOPT_FOLLOWLOCATION => false,
            CURLOPT_TIMEOUT_MS => 3000,
            CURLOPT_PROTOCOLS => CURLPROTO_HTTP,
            CURLOPT_HTTP_VERSION => CURL_HTTP_VERSION_1_1,
            CURLOPT_HEADERFUNCTION => static function (\CurlHandle $curl, string $line) use (&$response, &$finished): int {
                $code = curl_getinfo($curl, CURLINFO_RESPONSE_CODE);
                if (str_starts_with($line, 'HTTP/')) {
                    $response = trim(explode(' ', $line, 2)[1] ?? '');
                }
                if ($line === "\r\n" && ($code >= 200 || $code === 101)) {
                    // Liveness only needs the complete response headers, including their status.
                    $finished = true;

                    return 0;
                }

                return strlen($line);
            },
        ]);
        curl_exec($handle);

        if (!$finished) {
            throw new CommandError('health check: ' . curl_error($handle));
        }
        if (curl_getinfo($handle, CURLINFO_RESPONSE_CODE) !== 200) {
            throw new CommandError("health check: $target answered $response");
        }
    }
}
