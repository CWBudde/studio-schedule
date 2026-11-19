<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use StudioSchedule\Domain\Id;
use StudioSchedule\Service\PersonDetailReader;

final readonly class PersonDetailEndpoint
{
    public function __construct(private PersonDetailReader $people) {}

    /** @param array{personId:string} $args */
    public function __invoke(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        if (!Id::valid($args['personId'])) {
            return Responses::problem($request, 422, 'validation failed', errors: [
                ['location' => 'path.personId', 'message' => 'invalid ID'],
            ]);
        }
        $person = $this->people->get(strtolower($args['personId']));

        return $person === null ? Responses::problem($request, 404, 'resource not found')
            : Responses::json($response, $person)->withHeader('Cache-Control', 'no-store');
    }
}
