<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\ArrayParameterType;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Domain\Instant;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-type ReceiptRow array{
 *   id:string, number:string, kind:string, issued_at:string, issue_date:string, issuer_name:string, issuer_address:string,
 *   issuer_tax_id:string, issuer_email:?string, issuer_phone:?string, tax_mode:string, tax_note:?string,
 *   payer_person_id:?string, payer_name:?string, payer_anonymized_at:?string, recipient_name:string, recipient_address:?string,
 *   recipient_email:?string, payment_method:string, currency:string, net:int|numeric-string, vat:int|numeric-string,
 *   gross:int|numeric-string, reason:?string, original_id:?string, original_number:?string, original_date:?string,
 *   cancellation_id:?string, cancellation_number:?string, cancellation_date:?string, pass_id:?string,
 *   event_id:?string, event_title:?string, cancelable:bool|int|string
 * }
 * @phpstan-type LineRow array{
 *   receipt_id:string, position:int|numeric-string, description:string, quantity:int|numeric-string, unit_price:int|numeric-string,
 *   vat_rate_bp:int|numeric-string, net:int|numeric-string, vat:int|numeric-string, gross:int|numeric-string
 * }
 */
final readonly class ReceiptReadStore
{
    private const string ITEMS = <<<'SQL'
        SELECT r.id, r.number, r.kind, r.issued_at, r.issue_date, r.issuer_name, r.issuer_address, r.issuer_tax_id, r.issuer_email,
               r.issuer_phone, r.tax_mode, r.tax_note, r.payer_person_id, CONCAT(p.given_name, ' ', p.family_name) AS payer_name,
               p.anonymized_at AS payer_anonymized_at, r.recipient_name, r.recipient_address, r.recipient_email, r.payment_method,
               r.currency, r.net, r.vat, r.gross, r.reason, pass.id AS pass_id, e.id AS event_id, e.title AS event_title,
               original.id AS original_id, original.number AS original_number, original.issue_date AS original_date,
               cancellation.id AS cancellation_id, cancellation.number AS cancellation_number, cancellation.issue_date AS cancellation_date,
               (r.kind = 'sale' AND cancellation.id IS NULL
                 AND NOT EXISTS (SELECT 1 FROM pass_redemptions redemption WHERE redemption.pass_id = pass.id)
                 AND (reg.id IS NULL OR reg.status NOT IN ('waiting', 'invited', 'registered'))
                 AND NOT EXISTS (SELECT 1 FROM lessons l WHERE l.event_id = e.id AND l.status = 'scheduled' AND l.starts_at <= ?)
               ) AS cancelable
        FROM receipts r
        LEFT JOIN persons p ON p.id = r.payer_person_id
        LEFT JOIN receipts original ON original.id = r.cancels_receipt_id
        LEFT JOIN receipts cancellation ON cancellation.cancels_receipt_id = r.id
        LEFT JOIN passes pass ON pass.receipt_id = r.id
        LEFT JOIN event_registrations reg ON reg.receipt_id = COALESCE(r.cancels_receipt_id, r.id)
        LEFT JOIN events e ON e.id = reg.event_id
        SQL;
    private const string ORDER = ' ORDER BY r.year DESC, r.sequence DESC, r.id';
    public function __construct(private Connection $db, private ClockInterface $clock) {}

    /** @return array{items:list<array<string,mixed>>,total:int,years:list<int>} */
    public function search(int $year, #[SensitiveParameter] string $query, int $offset, int $limit): array
    {
        $filters = [];
        $parameters = [];
        if ($year > 0) {
            $filters[] = 'r.year = ?';
            $parameters[] = $year;
        }
        if ($query !== '') {
            $number = Database::asciiContains('r.number', $this->isPostgres());
            $recipient = Database::asciiContains('r.recipient_name', $this->isPostgres());
            $filters[] = "($number OR $recipient)";
            $folded = strtolower($query);
            array_push($parameters, $folded, $folded);
        }
        $where = $filters === [] ? '' : ' WHERE ' . implode(' AND ', $filters);

        return Database::inReadSnapshot($this->db, function () use ($where, $parameters, $offset, $limit): array {
            /** @var int|numeric-string $total */
            $total = $this->db->fetchOne('SELECT COUNT(*) FROM receipts r' . $where, $parameters);
            /** @var list<int|numeric-string> $years */
            $years = $this->db->fetchFirstColumn('SELECT year FROM receipt_counters ORDER BY year DESC');

            return [
                'items' => $this->items($where, $parameters, $limit, $offset),
                'total' => (int) $total,
                'years' => array_map(intval(...), $years),
            ];
        });
    }

    /** @return array<string,mixed>|null */
    public function detail(string $id): ?array
    {
        return Database::inReadSnapshot($this->db, fn(): ?array
            => $this->items(' WHERE r.id = ' . $this->idParameter(), [$this->idValue($id)])[0] ?? null);
    }

    /** @return list<array<string,mixed>> */
    public function mine(string $person): array
    {
        $parameter = $this->idParameter();
        $value = $this->idValue($person);
        $where = " WHERE r.payer_person_id IN (SELECT owner.id FROM persons owner WHERE owner.id = $parameter OR owner.merged_into = $parameter)";

        return Database::inReadSnapshot($this->db, fn(): array => $this->items($where, [$value, $value]));
    }

    /**
     * @param list<int|string> $parameters
     * @return list<array<string,mixed>>
     */
    private function items(string $where, array $parameters, ?int $limit = null, int $offset = 0): array
    {
        $now = Database::sqlInstant($this->db, $this->clock->now());
        $sql = self::ITEMS . $where . self::ORDER;
        $types = array_fill(0, count($parameters) + 1, ParameterType::STRING);
        if ($limit !== null) {
            $sql .= ' LIMIT ? OFFSET ?';
            $parameters[] = $limit;
            $parameters[] = min($offset, 2147483647);
            $types[] = ParameterType::INTEGER;
            $types[] = ParameterType::INTEGER;
        }
        /** @var list<ReceiptRow> $rows */
        $rows = $this->db->fetchAllAssociative($sql, [$now, ...$parameters], $types);
        $lines = $this->lines(array_column($rows, 'id'));

        return array_map(fn(array $row): array => $this->item($row, $lines[$row['id']] ?? []), $rows);
    }

    /**
     * @param list<string> $ids
     * @return array<string,list<array<string,int|string>>>
     */
    private function lines(array $ids): array
    {
        $grouped = [];
        foreach (array_chunk($ids, 1000) as $chunk) {
            /** @var list<LineRow> $rows */
            $rows = $this->db->fetchAllAssociative(
                'SELECT receipt_id, position, description, quantity, unit_price, vat_rate_bp, net, vat, gross '
                . 'FROM receipt_lines WHERE receipt_id IN (?) ORDER BY receipt_id, position',
                [$chunk],
                [$this->isPostgres() ? ArrayParameterType::STRING : ArrayParameterType::BINARY],
            );
            foreach ($rows as $row) {
                $grouped[$row['receipt_id']][] = [
                    'position' => (int) $row['position'], 'description' => $row['description'], 'quantity' => (int) $row['quantity'],
                    'unitPrice' => (int) $row['unit_price'], 'vatRate' => (int) $row['vat_rate_bp'],
                    'net' => (int) $row['net'], 'vat' => (int) $row['vat'], 'gross' => (int) $row['gross'],
                ];
            }
        }

        return $grouped;
    }

    /**
     * @param ReceiptRow $row
     * @param list<array<string,int|string>> $lines
     * @return array<string,mixed>
     */
    private function item(#[SensitiveParameter] array $row, array $lines): array
    {
        $item = [
            'id' => $this->id($row['id']), 'number' => $row['number'], 'kind' => $row['kind'],
            'issuedAt' => Instant::format(Database::instant($row['issued_at'])),
            'issueDate' => $row['issue_date'],
            'issuer' => ['name' => $row['issuer_name'], 'address' => $row['issuer_address'], 'taxNumber' => $row['issuer_tax_id']]
                + self::optional(['email' => $row['issuer_email'], 'phone' => $row['issuer_phone']]),
            'taxMode' => $row['tax_mode'], 'recipientName' => $row['recipient_name'], 'paymentMethod' => $row['payment_method'],
            'currency' => $row['currency'], 'net' => (int) $row['net'], 'vat' => (int) $row['vat'], 'gross' => (int) $row['gross'],
            'lines' => $lines, 'cancelable' => in_array($row['cancelable'], [true, 't', 1, '1'], true),
        ] + self::optional([
            'taxNote' => $row['tax_note'], 'recipientAddress' => $row['recipient_address'], 'recipientEmail' => $row['recipient_email'],
            'reason' => $row['reason'], 'passId' => $this->optionalId($row['pass_id']),
            'eventId' => $this->optionalId($row['event_id']), 'eventTitle' => $row['event_title'],
        ]);
        if ($row['payer_person_id'] !== null) {
            $item['payer'] = ['id' => $this->id($row['payer_person_id']), 'name' => $row['payer_name'] ?? ''];
            if ($row['payer_anonymized_at'] !== null) {
                $item['payer']['anonymized'] = true;
            }
        }
        if ($row['original_id'] !== null) {
            $item['cancels'] = ['id' => $this->id($row['original_id']), 'number' => $row['original_number'], 'issueDate' => $row['original_date']];
        }
        if ($row['cancellation_id'] !== null) {
            $item['canceledBy'] = [
                'id' => $this->id($row['cancellation_id']), 'number' => $row['cancellation_number'], 'issueDate' => $row['cancellation_date'],
            ];
        }

        return $item;
    }

    /**
     * @param array<string,?string> $fields
     * @return array<string,string>
     */
    private static function optional(array $fields): array
    {
        return array_filter($fields, static fn(?string $value): bool => $value !== null && $value !== '');
    }

    private function optionalId(?string $id): ?string
    {
        return $id === null ? null : $this->id($id);
    }

    private function id(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromBinary($id)->toRfc4122();
    }

    private function idValue(string $id): string
    {
        return $this->isPostgres() ? $id : str_replace('-', '', $id);
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private function idParameter(): string
    {
        return $this->isPostgres() ? '?' : 'UNHEX(?)';
    }
}
