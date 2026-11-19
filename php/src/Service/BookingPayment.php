<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

/** @phpstan-import-type PaymentPass from ParticipantReadPersistence */
final class BookingPayment
{
    /**
     * @param array{source:string,trial:bool,status:string,late:bool,pass:?PaymentPass} $booking
     * @return array{status:string,pass?:PaymentPass}
     */
    public static function document(array $booking, string $lessonStatus, bool $onBooking): array
    {
        if ($booking['pass'] !== null) {
            return ['status' => 'paid', 'pass' => $booking['pass']];
        }

        return ['status' => match (true) {
            $booking['source'] === 'member', $booking['trial'], $lessonStatus === 'canceled' => 'free',
            $booking['status'] === 'canceled' => $booking['late'] ? 'unpaid' : 'free',
            !$onBooking && $booking['status'] === 'booked' => 'pending',
            default => 'unpaid',
        }];
    }
}
