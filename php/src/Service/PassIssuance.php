<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use DateTimeZone;
use InvalidArgumentException;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Domain\Date;
use StudioSchedule\Receipt\Document;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type Settings from ReceiptWritePersistence
 * @phpstan-import-type Product from PassIssuePersistence
 * @phpstan-type Purchase array{payerId:?string,price:?int,method:'cash'|'transfer'|'card',name:string,address:string,email:string}
 */
final readonly class PassIssuance
{
    public function __construct(
        private PassIssuePersistence $store,
        private ReceiptIssuance $receipts,
        private ClockInterface $clock,
        private ?Closure $wakeMail = null,
    ) {}

    /**
     * @param Purchase|null $purchase
     * @return array<string,mixed>
     */
    public function issue(
        SessionPrincipal $principal,
        string $owner,
        string $type,
        ?Date $from,
        #[SensitiveParameter]
        string $note,
        #[SensitiveParameter]
        ?array $purchase = null,
    ): array {
        $result = $this->store->write(function () use ($principal, $owner, $type, $from, $note, $purchase): array {
            $settings = $this->receipts->settings();
            $payer = $purchase['payerId'] ?? $owner;
            if ($payer !== $owner && !$this->store->livePerson($payer)) {
                throw CatalogFailure::field('payerId', 'person does not exist');
            }
            if (!$this->store->lockLivePersons(...($payer === $owner ? [$owner] : [$owner, $payer]))) {
                throw new CatalogFailure(404, 'resource not found');
            }

            $product = $this->store->lockProduct($type) ?? throw CatalogFailure::field('passTypeId', 'pass type does not exist');
            if (!$product['active']) {
                throw CatalogFailure::field('passTypeId', 'the pass type is retired');
            }

            $now = $this->clock->now();
            $today = (string) Date::fromInstant($now, new DateTimeZone($settings['timezone']));
            $from ??= Date::parse($today);
            try {
                $until = $product['days'] === 0 ? null : (string) $from->addDays($product['days'] - 1);
            } catch (InvalidArgumentException) {
                throw CatalogFailure::field('validFrom', 'the pass validity falls outside the supported date range');
            }
            $actor = $principal->impersonatorId ?? $principal->accountId;
            $price = $purchase === null ? 0 : ($purchase['price'] ?? $product['price']);
            $receipt = $purchase === null ? null : $this->saleReceipt(
                $settings,
                $product,
                $actor,
                $now,
                $owner,
                $payer,
                (string) $from,
                $until,
                $price,
                $purchase,
            );
            $pass = $this->store->create([
                'id' => Uuid::v7()->toRfc4122(), 'pass_type_id' => $type, 'person_id' => $owner, 'kind' => $product['kind'],
                'visits_total' => $product['visits'], 'visits_remaining' => $product['visits'],
                'valid_from' => (string) $from, 'valid_until' => $until, 'status' => 'active',
                'origin' => $purchase === null ? 'assignment' : 'sale',
                'sold_at' => $now, 'sold_by' => $actor, 'payer_person_id' => $payer === $owner ? null : $payer,
                'receipt_id' => $receipt, 'price_paid' => $price, 'note' => $note === '' ? null : $note,
            ]);

            return $pass + ['usable' => PassRead::covers($pass, $today), 'uses' => [], 'useCount' => 0];
        });
        if ($purchase !== null && $this->wakeMail !== null) {
            ($this->wakeMail)();
        }

        return $result;
    }

    /**
     * @param Settings $settings
     * @param Product $product
     * @param Purchase $purchase
     */
    private function saleReceipt(
        array $settings,
        array $product,
        string $actor,
        DateTimeImmutable $now,
        string $ownerId,
        string $payerId,
        string $from,
        ?string $until,
        int $price,
        #[SensitiveParameter]
        array $purchase,
    ): string {
        ReceiptIssuance::requireIssuer($settings);
        $owner = $this->receipts->payer($ownerId);
        $payer = $ownerId === $payerId ? $owner : $this->receipts->payer($payerId);
        $ownerName = $owner['given_name'] . ' ' . $owner['family_name'];
        $name = $purchase['name'] !== '' ? $purchase['name'] : $payer['given_name'] . ' ' . $payer['family_name'];
        if (SetupInput::trim($name) === '' || mb_strlen($name, 'UTF-8') > 200) {
            throw CatalogFailure::field('recipientName', "the recipient's name must be 1 to 200 characters");
        }
        $email = $payer['email'] === null && $purchase['email'] !== '' ? $purchase['email'] : null;
        if ($email !== null && !SetupInput::validEmail($email)) {
            throw CatalogFailure::field('recipientEmail', 'invalid email address');
        }

        $item = static fn(string $lang): string => self::description($lang, $product['name'], $ownerName, $from, $until);
        $lines = [ReceiptIssuance::saleLine($item(Document::language($settings['default_locale'])), $price, $product['vatRate'], $settings['tax_mode'])];

        return $this->receipts->issue($settings, $actor, $now, [
            'kind' => 'sale', 'tax_mode' => $settings['tax_mode'] ?? '', 'tax_note' => $settings['tax_note'],
            'currency' => $settings['currency'], 'gross' => $price, 'recipient_name' => $name,
            'recipient_address' => $purchase['address'] === '' ? null : $purchase['address'], 'payment_method' => $purchase['method'],
            'payerId' => $payerId, 'recipientEmail' => $email,
        ], $lines, $item)['id'];
    }

    private static function description(string $lang, string $type, string $owner, string $from, ?string $until): string
    {
        $start = Document::formatDate($from, $lang);
        if ($until === null) {
            return $lang === 'de' ? "$type für $owner, gültig ab $start" : "$type for $owner, valid from $start";
        }
        $end = Document::formatDate($until, $lang);

        return $lang === 'de' ? "$type für $owner, gültig $start – $end" : "$type for $owner, valid $start – $end";
    }
}
