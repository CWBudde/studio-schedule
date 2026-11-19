<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\PersonDuplicates;
use StudioSchedule\Service\SessionPrincipal;

final readonly class DuplicateRoutes
{
    public function __construct(private PersonDuplicates $duplicates) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $office = CatalogAdminRoutes::requireOffice(...);
        $app->get('/api/v1/admin/duplicates', $this->list(...))->add($office);
        $app->post('/api/v1/admin/duplicates/dismissals', $this->dismiss(...))->add($office);
    }

    private function list(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        return Responses::json($response, ['items' => $this->duplicates->list()]);
    }

    private function dismiss(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $body = JsonObject::read($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $names = ['persona' => 'personA', 'personb' => 'personB'];
        $fields = $errors = [];
        foreach (get_object_vars($body) as $key => $value) {
            $field = strtolower((string) $key);
            if (!isset($names[$field])) {
                $errors[] = ['location' => 'body.' . $key, 'message' => 'unexpected property'];
                continue;
            }
            $fields[$field] = $value;
            if (!is_string($value) || !Id::valid($value)) {
                $errors[] = ['location' => 'body.' . $names[$field], 'message' => 'invalid ID'];
            }
        }
        foreach (array_diff_key($names, $fields) as $name) {
            $errors[] = ['location' => 'body.' . $name, 'message' => 'invalid ID'];
        }
        if ($errors !== []) {
            return Responses::problem($request, 422, 'validation failed', errors: $errors);
        }
        $a = $fields['persona'] ?? null;
        $b = $fields['personb'] ?? null;
        assert(is_string($a) && is_string($b));
        $a = strtolower($a);
        $b = strtolower($b);
        if ($a === $b) {
            return Responses::problem($request, 422, 'validation failed', errors: [
                ['location' => 'body.personB', 'message' => 'the persons must differ'],
            ]);
        }
        $actor = $request->getAttribute('sessionPrincipal');
        assert($actor instanceof SessionPrincipal);

        return $this->duplicates->dismiss($a, $b, $actor->impersonatorId ?? $actor->accountId)
            ? $response->withStatus(204)
            : Responses::problem($request, 404, 'resource not found');
    }
}
