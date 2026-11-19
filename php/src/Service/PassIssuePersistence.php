<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;

/**
 * @phpstan-import-type PassDocument from PassReadPersistence
 * @phpstan-type Product array{name:string,kind:string,visits:?int,days:int,price:int,vatRate:int,active:bool}
 * @phpstan-type NewPass array{
 *     id:string,pass_type_id:string,person_id:string,kind:string,visits_total:?int,visits_remaining:?int,
 *     valid_from:string,valid_until:?string,status:'active',origin:'sale'|'assignment',
 *     sold_at:DateTimeImmutable,sold_by:string,payer_person_id:?string,receipt_id:?string,price_paid:int,note:?string
 * }
 */
interface PassIssuePersistence
{
    /**
     * @template T
     * @param Closure():T $operation
     * @return T
     */
    public function write(Closure $operation): mixed;

    public function livePerson(string $id): bool;

    public function lockLivePersons(string ...$ids): bool;

    /** @return Product|null */
    public function lockProduct(string $id): ?array;

    /**
     * @param NewPass $pass
     * @return PassDocument
     */
    public function create(array $pass): array;
}
