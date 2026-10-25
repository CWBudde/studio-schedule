-- name: GetSettings :one
-- Everything but the logo, which only receipts read (GetSettingsLogo).
SELECT
    school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time,
    release_weeks_ahead, booking_cutoff, cancellation_cutoff, default_visible_days, legal_notice,
    waitlist_invite_expiry, redemption_mode, issuer_name, issuer_address, issuer_tax_id, issuer_email,
    issuer_phone, tax_mode, tax_note, receipt_footer
FROM settings;

-- name: PutSettings :exec
INSERT INTO settings (
    school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time,
    release_weeks_ahead, booking_cutoff, cancellation_cutoff, default_visible_days, legal_notice,
    waitlist_invite_expiry, redemption_mode, issuer_name, issuer_address, issuer_tax_id, issuer_email,
    issuer_phone, tax_mode, tax_note, receipt_footer
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22
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
    redemption_mode = excluded.redemption_mode,
    issuer_name = excluded.issuer_name,
    issuer_address = excluded.issuer_address,
    issuer_tax_id = excluded.issuer_tax_id,
    issuer_email = excluded.issuer_email,
    issuer_phone = excluded.issuer_phone,
    tax_mode = excluded.tax_mode,
    tax_note = excluded.tax_note,
    receipt_footer = excluded.receipt_footer;

-- name: GetSettingsLogo :one
SELECT logo, logo_type FROM settings;

-- name: PutSettingsLogo :execrows
UPDATE settings SET logo = sqlc.narg(logo), logo_type = sqlc.narg(logo_type);
