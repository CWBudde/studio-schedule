<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use SensitiveParameter;
use StudioSchedule\Domain\EventRegistration;
use StudioSchedule\Domain\PersonName;

/**
 * @phpstan-import-type Recipient from EventWaitlistPersistence
 * @phpstan-import-type Guest from EventRegistration
 * @phpstan-type Party array{personId:?string,guest:?Guest,refunded?:bool}
 * @phpstan-type Message array{recipient:Recipient,participants:list<string>,refunded:bool}
 */
final readonly class ScheduleRecipients
{
    public function __construct(private EventWaitlistPersistence $store) {}

    /**
     * @param list<Party> $participants
     * @return list<Message>
     */
    public function group(#[SensitiveParameter] array $participants): array
    {
        $ids = array_values(array_filter(array_column($participants, 'personId'), is_string(...)));
        $people = $this->store->recipients($ids);
        $messages = [];
        foreach ($participants as $participant) {
            $recipients = $people[$participant['personId'] ?? ''] ?? [];
            $guest = $participant['guest'];
            if ($guest !== null && isset($guest['email']) && $guest['email'] !== '') {
                $recipients = [[
                    'email' => $guest['email'], 'locale' => '', 'accountId' => null,
                    'givenName' => $guest['name'], 'participant' => $guest['name'],
                ]];
            }
            foreach ($recipients as $recipient) {
                $address = PersonName::lower($recipient['email']);
                $messages[$address] ??= ['recipient' => $recipient, 'participants' => [], 'refunded' => false];
                if ($recipient['accountId'] !== null && $messages[$address]['recipient']['accountId'] === null) {
                    $messages[$address]['recipient'] = $recipient;
                }
                if (!in_array($recipient['participant'], $messages[$address]['participants'], true)) {
                    $messages[$address]['participants'][] = $recipient['participant'];
                }
                $messages[$address]['refunded'] = $messages[$address]['refunded'] || ($participant['refunded'] ?? false);
            }
        }

        return array_values($messages);
    }
}
