<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use RuntimeException;

/**
 * @phpstan-import-type Counts from MemberImportPersistence
 * @phpstan-import-type ImportRow from MemberImportBatch
 */
final class MemberImportPreview extends RuntimeException
{
    /** @param array{applied:false,counts:Counts,rows:list<ImportRow>} $result */
    public function __construct(public readonly array $result)
    {
        parent::__construct('Roll back the import preview');
    }
}
