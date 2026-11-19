<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use RuntimeException;
use StudioSchedule\Service\PersonDetailReader;

final readonly class PersonDetailStore implements PersonDetailReader
{
    public function __construct(
        private Connection $db,
        private PersonExportStore $people,
        private CatalogStore $catalog,
    ) {}

    public function get(string $id): ?array
    {
        return Database::inReadSnapshot($this->db, function () use ($id): ?array {
            $person = $this->people->person($id);
            if ($person === null) {
                return null;
            }
            $links = $this->people->familyLinks($id, includeMatchFields: true);
            $detail = $person + [
                'guardians' => $links['guardians'],
                'dependents' => $links['dependents'],
                'staff' => $this->catalog->staffForPerson($id),
                'classes' => $this->people->classMemberships($id),
                'merged' => $links['mergedPersons'],
            ];

            $survivorId = $detail['mergedIntoId'] ?? null;
            if (is_string($survivorId)) {
                $survivor = $this->people->person($survivorId) ?? throw new RuntimeException('Missing merge survivor');
                $detail['mergedInto'] = array_intersect_key($survivor, array_flip(['id', 'givenName', 'familyName', 'email', 'memberNo']));
                if (isset($survivor['anonymizedAt'])) {
                    $detail['mergedInto']['anonymized'] = true;
                }
                unset($detail['mergedIntoId']);
            }

            $account = $this->people->account($id);
            if ($account !== null) {
                $detail['account'] = [
                    'id' => $account['id'], 'email' => $account['email'], 'role' => $account['role'],
                    'status' => isset($account['disabledAt']) ? 'disabled' : (isset($account['confirmedAt']) ? 'active' : 'pending'),
                ];
            }

            return $detail;
        });
    }
}
