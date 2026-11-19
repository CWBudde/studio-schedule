<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use InvalidArgumentException;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use Psr\Http\Server\RequestHandlerInterface;
use SensitiveParameter;
use Slim\App;
use Slim\Routing\RouteCollectorProxy;
use StudioSchedule\Mail\TemplateData;
use StudioSchedule\Service\MailLog;
use StudioSchedule\Service\SessionPrincipal;

final readonly class MailLogRoutes
{
    public function __construct(private MailLog $service) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $app->group('/api/v1/admin/mails', function (RouteCollectorProxy $routes): void {
            $routes->post('/search', $this->search(...));
            $routes->get('/{mailId}', $this->detail(...));
            $routes->post('/{mailId}/requeue', $this->requeue(...));
        })->add(self::requireAdmin(...));
    }

    private static function requireAdmin(#[SensitiveParameter] ServerRequestInterface $request, RequestHandlerInterface $next): ResponseInterface
    {
        $principal = $request->getAttribute('sessionPrincipal');
        if (!$principal instanceof SessionPrincipal) {
            return Responses::problem($request, 401, 'not signed in');
        }
        if ($principal->role !== 'admin') {
            return Responses::problem($request, 403, 'forbidden');
        }

        return $next->handle($request);
    }

    private function search(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $body = JsonObject::read($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $fields = array_change_key_case(get_object_vars($body));
        if (array_diff(array_keys($fields), ['status', 'template', 'recipient', 'cursor']) !== []) {
            return self::invalid($request, 'body', 'unexpected property');
        }
        $status = $fields['status'] ?? null;
        if ($status !== null && !in_array($status, ['pending', 'sent', 'failed'], true)) {
            return self::invalid($request, 'body.status', 'expected pending, sent or failed');
        }
        $template = $fields['template'] ?? null;
        if ($template !== null && (!is_string($template) || !TemplateData::isLogTemplate($template))) {
            return self::invalid($request, 'body.template', 'expected a mail template');
        }
        $recipient = $fields['recipient'] ?? '';
        if (!is_string($recipient) || mb_strlen($recipient, 'UTF-8') > 254 || str_contains($recipient, "\0")) {
            return self::invalid($request, 'body.recipient', 'expected a recipient of at most 254 characters');
        }
        $cursor = $fields['cursor'] ?? '';
        if (!is_string($cursor) || strlen($cursor) > 200) {
            return self::invalid($request, 'body.cursor', 'invalid cursor');
        }

        try {
            $page = $this->service->search($status ?? '', $template ?? '', $recipient, $cursor);
        } catch (InvalidArgumentException) {
            return self::invalid($request, 'body.cursor', 'invalid cursor');
        }

        return Responses::json($response, $page);
    }

    /** @param array{mailId:string} $args */
    private function detail(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        if (!MailLog::validId($args['mailId'])) {
            return self::invalid($request, 'path.mailId', 'invalid ID');
        }
        $mail = $this->service->detail(strtolower($args['mailId']));

        return $mail === null
            ? Responses::problem($request, 404, 'resource not found')
            : Responses::json($response, $mail, $request->getMethod() === 'HEAD');
    }

    /** @param array{mailId:string} $args */
    private function requeue(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        if (!MailLog::validId($args['mailId'])) {
            return self::invalid($request, 'path.mailId', 'invalid ID');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);
        $result = $this->service->requeue(strtolower($args['mailId']), $principal);

        return match ($result) {
            null => Responses::problem($request, 404, 'resource not found'),
            'secret-link' => Responses::problem($request, 409, 'a mail with a secret link is not queued again', reason: $result),
            'not-failed' => Responses::problem($request, 409, 'only a failed mail is queued again', reason: $result),
            default => Responses::json($response, $result),
        };
    }

    private static function invalid(ServerRequestInterface $request, string $location, string $message): ResponseInterface
    {
        return Responses::problem($request, 422, 'validation failed', errors: [['location' => $location, 'message' => $message]]);
    }
}
