<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Base64Url\Base64Url;
use InvalidArgumentException;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Push\Sender;
use StudioSchedule\Push\StatusError;
use StudioSchedule\Store\PushOutboxStore;
use Throwable;

/** @phpstan-import-type PushClaim from PushOutboxStore */
final readonly class PushDelivery
{
    private const int BATCH_SIZE = 50;

    public function __construct(private PushOutboxStore $outbox, private ClockInterface $clock, private Sender $sender) {}

    public function deliver(): int
    {
        $sent = 0;
        for ($i = 0; $i < self::BATCH_SIZE; ++$i) {
            $claim = $this->outbox->claim($this->clock->now());
            if ($claim === null) {
                break;
            }
            if (isset($claim['discarded'])) {
                continue;
            }

            [$error, $permanent, $gone] = $this->send($claim);
            if ($this->outbox->record($claim, $this->clock->now(), $error, $permanent, $gone)) {
                ++$sent;
            }
        }

        return $sent;
    }

    /**
     * @param PushClaim $claim
     * @return array{?string,bool,bool}
     */
    private function send(#[SensitiveParameter] array $claim): array
    {
        $subscription = $claim['subscription'];
        try {
            $this->sender->send(
                $subscription['endpoint'],
                Base64Url::encode((string) hex2bin($subscription['public_key'])),
                Base64Url::encode((string) hex2bin($subscription['auth_key'])),
                $claim['payload'],
                $claim['ttl'],
                'high',
            );
        } catch (StatusError $error) {
            // A provider body can echo the endpoint or contain other private data.
            return ['Push service returned HTTP ' . $error->statusCode, $error->permanent(), $error->gone()];
        } catch (InvalidArgumentException) {
            return ['Push message or subscription is invalid', true, false];
        } catch (Throwable) {
            return ['Push delivery failed', false, false];
        }

        return [null, false, false];
    }
}
