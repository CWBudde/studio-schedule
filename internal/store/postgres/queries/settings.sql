-- name: GetSettings :one
SELECT * FROM settings;

-- name: PutSettings :exec
INSERT INTO settings (
    school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time,
    release_weeks_ahead, booking_cutoff, cancellation_cutoff, default_visible_days, legal_notice,
    waitlist_invite_expiry, redemption_mode
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
ON CONFLICT (id) DO UPDATE SET
    school_name = excluded.school_name,
    timezone = excluded.timezone,
    default_locale = excluded.default_locale,
    currency = excluded.currency,
    receipt_prefix = excluded.receipt_prefix,
    release_weekday = excluded.release_weekday,
    release_time = excluded.release_time,
    release_weeks_ahead = excluded.release_weeks_ahead,
    booking_cutoff = excluded.booking_cutoff,
    cancellation_cutoff = excluded.cancellation_cutoff,
    default_visible_days = excluded.default_visible_days,
    legal_notice = excluded.legal_notice,
    waitlist_invite_expiry = excluded.waitlist_invite_expiry,
    redemption_mode = excluded.redemption_mode;
