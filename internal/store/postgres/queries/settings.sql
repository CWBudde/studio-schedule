-- name: GetSettings :one
-- Everything but the logo, which only receipts read (GetSettingsLogo).
SELECT
    school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time,
    release_weeks_ahead, booking_cutoff, cancellation_cutoff, default_visible_days, legal_notice,
    waitlist_invite_expiry, redemption_mode, issuer_name, issuer_address, issuer_tax_id, issuer_email,
    issuer_phone, tax_mode, tax_note, receipt_footer, accent_color
FROM settings;

-- name: PutSettings :exec
INSERT INTO settings (
    school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time,
    release_weeks_ahead, booking_cutoff, cancellation_cutoff, default_visible_days, legal_notice,
    waitlist_invite_expiry, redemption_mode, issuer_name, issuer_address, issuer_tax_id, issuer_email,
    issuer_phone, tax_mode, tax_note, receipt_footer, accent_color
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23
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
    receipt_footer = excluded.receipt_footer,
    accent_color = excluded.accent_color;

-- name: GetSettingsLogo :one
SELECT logo, logo_type FROM settings;

-- name: PutSettingsLogo :execrows
UPDATE settings SET logo = sqlc.narg(logo), logo_type = sqlc.narg(logo_type), logo_digest = sqlc.narg(logo_digest);

-- name: GetSettingsLogoInfo :one
-- The logo's type and digest, without reading the logo.
SELECT logo_type, logo_digest FROM settings;

-- name: SettingsUsage :one
-- What fixes the time zone (lessons) and the currency (prices, receipts).
SELECT
    EXISTS (SELECT 1 FROM lessons) AS lessons,
    (EXISTS (SELECT 1 FROM pass_types WHERE price > 0) OR EXISTS (SELECT 1 FROM passes WHERE price_paid > 0))::boolean
        AS prices,
    EXISTS (SELECT 1 FROM receipts) AS receipts;
