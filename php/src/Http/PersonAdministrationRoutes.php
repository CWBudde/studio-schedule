<?php

declare(strict_types=1);

namespace StudioSchedule\Http;

use InvalidArgumentException;
use Psr\Container\ContainerInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\ServerRequestInterface;
use SensitiveParameter;
use Slim\App;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Id;
use StudioSchedule\Domain\PersonData;
use StudioSchedule\Domain\PersonRecord;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\PersonAdministration;
use StudioSchedule\Service\SettingsNotFound;

final readonly class PersonAdministrationRoutes
{
    private const string PEOPLE = '/api/v1/admin/persons';

    public function __construct(private PersonAdministration $people) {}

    /**
     * @template TContainer of ContainerInterface|null
     * @param App<TContainer> $app
     */
    public function register(App $app): void
    {
        $office = CatalogAdminRoutes::requireOffice(...);
        $app->post(self::PEOPLE, $this->save(...))->add($office);
        $app->put(self::PEOPLE . '/{personId}', $this->save(...))->add($office);
        $app->post(self::PEOPLE . '/{personId}/guardians', $this->addGuardian(...))->add($office);
        $app->delete(self::PEOPLE . '/{personId}/guardians/{guardianId}', $this->removeGuardian(...))->add($office);
    }

    /** @param array{personId?:string} $args */
    private function save(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = isset($args['personId']) ? self::id($args['personId'], 'personId') : null;
        $body = CatalogAdminRoutes::fields($request, ['givenname', 'familyname', 'email', 'phone', 'birthdate', 'notes', 'memberno']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $given = CatalogAdminRoutes::text($body, 'givenName', 100, required: true);
        $family = CatalogAdminRoutes::text($body, 'familyName', 100, required: true);
        $phone = CatalogAdminRoutes::text($body, 'phone', 50) ?? '';
        $date = CatalogAdminRoutes::text($body, 'birthDate', 10);
        try {
            $birth = $date === null ? Date::fromText('') : Date::parse($date);
        } catch (InvalidArgumentException) {
            throw CatalogFailure::field('birthDate', 'expected a calendar date in YYYY-MM-DD form');
        }
        $record = new PersonRecord(
            new PersonData($given, $family, $phone, $birth),
            CatalogAdminRoutes::text($body, 'email', 254) ?? '',
            CatalogAdminRoutes::text($body, 'notes', 2000) ?? '',
            CatalogAdminRoutes::text($body, 'memberNo', 40) ?? '',
        );

        try {
            $person = $this->people->save($id, $record);
        } catch (SettingsNotFound) {
            throw new CatalogFailure(404, 'resource not found');
        }

        return Responses::json($response->withStatus($id === null ? 201 : 200), $person)->withHeader('Cache-Control', 'no-store');
    }

    /** @param array{personId:string} $args */
    private function addGuardian(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $id = self::id($args['personId'], 'personId');
        $body = CatalogAdminRoutes::fields($request, ['guardianid']);
        if ($body instanceof ResponseInterface) {
            return $body;
        }
        $guardian = $this->people->addGuardian($id, CatalogAdminRoutes::requiredId($body, 'guardianId'));

        return Responses::json($response->withStatus(201), $guardian)->withHeader('Cache-Control', 'no-store');
    }

    /** @param array{personId:string,guardianId:string} $args */
    private function removeGuardian(#[SensitiveParameter] ServerRequestInterface $request, ResponseInterface $response, array $args): ResponseInterface
    {
        $this->people->removeGuardian(self::id($args['personId'], 'personId'), self::id($args['guardianId'], 'guardianId'));

        return $response->withStatus(204);
    }

    private static function id(string $value, string $field): string
    {
        if (!Id::valid($value)) {
            throw new CatalogFailure(422, 'invalid ID', "path.$field");
        }

        return strtolower($value);
    }
}
