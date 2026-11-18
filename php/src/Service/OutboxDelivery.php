<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Mail\DeliveryFailure;
use StudioSchedule\Mail\Mailer;
use StudioSchedule\Store\OutboxStore;
use Throwable;

/** @phpstan-import-type OutboxClaim from OutboxStore */
final readonly class OutboxDelivery
{
    private const int BATCH_SIZE = 20;

    /** @var (Closure(string): ?array{filename:string,pdf:string})|null */
    private ?Closure $receiptReader;

    /** @param (callable(string): ?array{filename:string,pdf:string})|null $receiptReader */
    public function __construct(
        private OutboxStore $store,
        private ClockInterface $clock,
        private ?Mailer $mailer,
        ?callable $receiptReader = null,
    ) {
        $this->receiptReader = $receiptReader === null ? null : Closure::fromCallable($receiptReader);
    }

    public function deliver(): int
    {
        if ($this->mailer === null) {
            return 0;
        }

        $this->store->retireExpiredLastAttempts($this->clock->now());
        $sent = 0;
        for ($i = 0; $i < self::BATCH_SIZE; ++$i) {
            $claim = $this->store->claim($this->clock->now());
            if ($claim === null) {
                break;
            }

            $failure = $this->send($this->mailer, $claim);
            $recorded = $this->store->record(
                $claim,
                $this->clock->now(),
                $failure['error'] ?? null,
                $failure['permanent'] ?? false,
            );
            if ($recorded && $failure === null) {
                ++$sent;
            }
        }

        return $sent;
    }

    /**
     * @param OutboxClaim $claim
     * @return array{error:string,permanent:bool}|null
     */
    private function send(Mailer $mailer, #[SensitiveParameter] array $claim): ?array
    {
        if ($claim['text'] === null || preg_match('/\S/u', $claim['text']) !== 1) {
            return ['error' => 'Message body unavailable', 'permanent' => true];
        }

        $receipt = null;
        if ($claim['receiptId'] !== null) {
            try {
                $receipt = $this->receiptReader === null ? null : ($this->receiptReader)($claim['receiptId']);
            } catch (Throwable) {
                // Receipt lookup errors can contain database or filesystem details.
            }
            if ($receipt === null || trim($receipt['filename']) === '' || $receipt['pdf'] === '') {
                return ['error' => 'Receipt PDF unavailable', 'permanent' => false];
            }
        }

        try {
            $mailer->send($claim['recipient'], $claim['subject'], $claim['text'], $claim['html'], $claim['replyTo'], $receipt['filename'] ?? null, $receipt['pdf'] ?? null);
        } catch (DeliveryFailure $error) {
            return ['error' => $error->permanent ? 'Mail delivery rejected' : 'Mail delivery failed', 'permanent' => $error->permanent];
        } catch (Throwable) {
            return ['error' => 'Mail delivery failed', 'permanent' => false];
        }

        return null;
    }
}
