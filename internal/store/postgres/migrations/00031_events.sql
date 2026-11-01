-- Events and workshops (ADR 0019): events sold as one thing at their own
-- price, their images, their sessions (lessons of kind 'workshop' with
-- event_id set) and the registrations of persons and guests for them.
-- Conventions as in 00001_core.sql, except for the foreign keys to accounts,
-- which forget a deleted account (SET NULL) as those of 00023 do, and the
-- image, which goes with its event.
--
-- The checks mirror domain.Event.Validate and
-- domain.EventRegistration.Validate. An event's image is stored like the
-- school's background (00025_settings_theme_background.sql), in a table of
-- its own so that reading events never reads it. A person has at most one
-- open (waiting, invited or registered) registration per event
-- (event_registrations_one_open_key); an invite token hash, a receipt and a
-- Storno belong to one registration each. An event with sessions or
-- registrations, canceled ones included, cannot be deleted (RESTRICT).

-- +goose Up

CREATE TABLE events (
    id uuid PRIMARY KEY,
    -- domain.ValidEventSlug: groups of lowercase ASCII letters and digits
    -- joined by single hyphens, at most 80 characters.
    slug text COLLATE "C" NOT NULL
        CONSTRAINT events_slug_format CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(slug) <= 80),
    title text NOT NULL CHECK (btrim(title) <> ''),
    description text CHECK (description <> ''),
    course_type_id uuid NOT NULL REFERENCES course_types ON DELETE RESTRICT,
    capacity integer NOT NULL CHECK (capacity BETWEEN 1 AND 10000),
    -- Gross, in minor units of settings.currency.
    price bigint NOT NULL CHECK (price BETWEEN 0 AND 100000000),
    -- Basis points: 1900 is 19 %.
    vat_rate_bp integer NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    status text NOT NULL CHECK (status IN ('draft', 'published', 'canceled')),
    -- The sign-up window and the deadline of a member's own cancellation,
    -- each NULL for the default.
    signup_opens_at timestamptz,
    signup_closes_at timestamptz,
    cancel_until timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    created_by uuid REFERENCES accounts ON DELETE SET NULL,
    CONSTRAINT events_slug_key UNIQUE (slug),
    CONSTRAINT events_signup_window CHECK (signup_closes_at > signup_opens_at),
    CONSTRAINT events_updated_after_created CHECK (updated_at >= created_at)
);

CREATE INDEX events_course_type_idx ON events (course_type_id);
CREATE INDEX events_created_by_idx ON events (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE event_images (
    event_id uuid PRIMARY KEY REFERENCES events ON DELETE CASCADE,
    -- At most domain.MaxBackgroundBytes.
    image bytea NOT NULL CHECK (octet_length(image) BETWEEN 1 AND 1048576),
    image_type text NOT NULL CHECK (image_type IN ('image/jpeg', 'image/png', 'image/webp')),
    -- domain.Background.Digest, as settings.background_digest. Written with
    -- the image, so a cache check need not read it.
    image_digest text NOT NULL CHECK (image_digest ~ '^[0-9a-f]{64}$')
);

ALTER TABLE lessons
    -- NULL for a lesson of no event; an event's session is a one-off
    -- workshop.
    ADD COLUMN event_id uuid REFERENCES events ON DELETE RESTRICT,
    ADD CONSTRAINT lessons_event_session CHECK (event_id IS NULL OR (kind = 'workshop' AND class_id IS NULL));

CREATE INDEX lessons_event_idx ON lessons (event_id, starts_at, id) WHERE event_id IS NOT NULL;

CREATE TABLE event_registrations (
    id uuid PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES events ON DELETE RESTRICT,
    -- NULL for a guest, who has a name and (until anonymized) an email
    -- instead.
    person_id uuid REFERENCES persons ON DELETE RESTRICT,
    guest_name text CHECK (btrim(guest_name) <> ''),
    guest_email text CHECK (guest_email <> ''),
    status text NOT NULL CHECK (status IN ('waiting', 'invited', 'registered', 'canceled')),
    payment text NOT NULL CHECK (payment IN ('unpaid', 'paid', 'refunded')),
    -- The event's price and VAT rate when the registration was made.
    price bigint NOT NULL CHECK (price BETWEEN 0 AND 100000000),
    vat_rate_bp integer NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    -- The receipt of the payment and its Storno.
    receipt_id uuid REFERENCES receipts ON DELETE RESTRICT,
    cancellation_receipt_id uuid REFERENCES receipts ON DELETE RESTRICT,
    -- The invite of the waiting list, set exactly while invited.
    invite_token_hash bytea CHECK (octet_length(invite_token_hash) = 32),
    invited_at timestamptz,
    expires_at timestamptz,
    registered_by uuid REFERENCES accounts ON DELETE SET NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    canceled_at timestamptz,
    CONSTRAINT event_registrations_person_or_guest CHECK (
        (person_id IS NOT NULL AND guest_name IS NULL AND guest_email IS NULL)
        OR (person_id IS NULL AND guest_name IS NOT NULL)
    ),
    CONSTRAINT event_registrations_invite CHECK (
        CASE WHEN status = 'invited'
            THEN invite_token_hash IS NOT NULL AND invited_at IS NOT NULL AND expires_at IS NOT NULL
                AND expires_at > invited_at
            ELSE invite_token_hash IS NULL AND invited_at IS NULL AND expires_at IS NULL
        END
    ),
    CONSTRAINT event_registrations_canceled CHECK ((status = 'canceled') = (canceled_at IS NOT NULL)),
    CONSTRAINT event_registrations_unpaid_no_receipt CHECK (payment <> 'unpaid' OR receipt_id IS NULL),
    CONSTRAINT event_registrations_storno CHECK (
        (payment = 'refunded') = (cancellation_receipt_id IS NOT NULL)
        AND (payment <> 'refunded' OR receipt_id IS NOT NULL)
    ),
    CONSTRAINT event_registrations_storno_other CHECK (cancellation_receipt_id <> receipt_id),
    CONSTRAINT event_registrations_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT event_registrations_receipt_key UNIQUE (receipt_id),
    CONSTRAINT event_registrations_cancellation_receipt_key UNIQUE (cancellation_receipt_id),
    CONSTRAINT event_registrations_token_key UNIQUE (invite_token_hash)
);

-- A person has at most one open registration per event.
CREATE UNIQUE INDEX event_registrations_one_open_key ON event_registrations (event_id, person_id)
    WHERE person_id IS NOT NULL AND status IN ('waiting', 'invited', 'registered');
CREATE INDEX event_registrations_event_idx ON event_registrations (event_id, created_at, id);
CREATE INDEX event_registrations_person_idx ON event_registrations (person_id, created_at, id)
    WHERE person_id IS NOT NULL;
CREATE INDEX event_registrations_registered_by_idx ON event_registrations (registered_by)
    WHERE registered_by IS NOT NULL;
-- The guests the retention job anonymizes.
CREATE INDEX event_registrations_guest_idx ON event_registrations (event_id) WHERE person_id IS NULL;

-- +goose Down

DROP TABLE event_registrations;

DROP INDEX lessons_event_idx;
ALTER TABLE lessons DROP CONSTRAINT lessons_event_session,
    DROP COLUMN event_id;

DROP TABLE event_images;
DROP TABLE events;
