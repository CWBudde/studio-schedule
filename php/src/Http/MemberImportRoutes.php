<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use stdClass;
use StudioSchedule\Service\MemberImportMapping;
use StudioSchedule\Service\MemberImportProblem;
use StudioSchedule\Service\MemberImports;
use StudioSchedule\Service\SessionPrincipal;

final readonly class MemberImportRoutes
{
    public function __construct(private MemberImports $imports) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $office = CatalogAdminRoutes::requireOffice(...);
        $app->post('/api/v1/admin/imports/inspect', new MemberImportEndpoint())->add($office);
        $app->post('/api/v1/admin/imports', $this->run(...))->add($office);
        $app->get('/api/v1/admin/imports', $this->history(...))->add($office);
        $app->post('/api/v1/admin/persons/search', $this->search(...))->add($office);
    }

    private function run(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $upload = MemberImportEndpoint::readUpload($request, ['mapping', 'apply']);
        if ($upload instanceof ResponseInterface) {
            return $upload;
        }
        $apply = $upload['fields']['apply'] ?? false;
        if (!is_bool($apply)) {
            return MemberImportEndpoint::invalid($request, 'apply', 'expected a boolean');
        }
        $mapping = $upload['fields']['mapping'] ?? null;
        if (!$mapping instanceof stdClass) {
            return MemberImportEndpoint::invalid($request, 'mapping', 'expected an object');
        }
        $mapped = array_change_key_case(get_object_vars($mapping));
        if (array_diff(array_keys($mapped), ['columns', 'notes']) !== []) {
            return MemberImportEndpoint::invalid($request, 'mapping', 'unexpected property');
        }
        $columns = $mapped['columns'] ?? null;
        $notes = $mapped['notes'] ?? [];
        if (!$columns instanceof stdClass) {
            return MemberImportEndpoint::invalid($request, 'mapping.columns', 'expected an object');
        }
        if (!is_array($notes) || !array_is_list($notes)) {
            return MemberImportEndpoint::invalid($request, 'mapping.notes', 'expected an array of header names');
        }
        $principal = $request->getAttribute('sessionPrincipal');
        assert($principal instanceof SessionPrincipal);
        $csv = $upload['csv'];
        try {
            $resolved = MemberImportMapping::resolve($csv->headers, get_object_vars($columns), $notes);
            $result = $this->imports->run($upload['name'], $csv, $resolved, $apply, $principal->impersonatorId ?? $principal->accountId);
        } catch (MemberImportProblem $failure) {
            return MemberImportEndpoint::invalid($request, $failure->field, $failure->getMessage());
        }

        return Responses::json($response, $result);
    }

    private function history(ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        return Responses::json($response, ['items' => $this->imports->runs()], $request->getMethod() === 'HEAD');
    }

    private function search(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response): ResponseInterface
    {
        $body = JsonObject::read($request);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $fields = array_change_key_case(get_object_vars($body));
        if (array_diff(array_keys($fields), ['query']) !== []) {
            return Responses::problem($request, 422, 'unexpected property');
        }
        $query = $fields['query'] ?? '';
        if (!is_string($query) || mb_strlen($query, 'UTF-8') > 100) {
            return MemberImportEndpoint::invalid($request, 'query', 'expected text of at most 100 characters');
        }

        return Responses::json($response, ['items' => $this->imports->searchPeople($query)]);
    }
}
