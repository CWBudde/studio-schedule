-- Core aggregates: settings, people, catalog, timetable, lessons, bookings.
--
-- Conventions (docs/decisions/0002-domain-and-persistence-conventions.md):
--   - Enums are text columns with a CHECK constraint listing the Go values.
--   - Optional values are NULL, never empty strings or zero IDs; the CHECK
--     constraints on optional text keep '' out.
--   - Names use collation "C", so ORDER BY compares byte-wise.
--   - Foreign keys are ON DELETE RESTRICT: deleting a referenced record fails.
--   - CHECK constraints are backstops for bugs, not validation: services
--     validate entities before writing, so the checks are never stricter
--     than Validate().
--
-- Constraint names are referenced by the error mapping in errors.go.

-- +goose Up

CREATE TABLE settings (
    -- A single row: the key can only be true.
    id boolean PRIMARY KEY DEFAULT true CONSTRAINT settings_singleton CHECK (id),
    school_name text NOT NULL CHECK (btrim(school_name) <> ''),
    timezone text NOT NULL CHECK (timezone <> ''),
    default_locale text NOT NULL CHECK (btrim(default_locale) <> ''),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    receipt_prefix text NOT NULL CHECK (receipt_prefix ~ '^[A-Z0-9]{1,16}$'),
    release_weekday smallint NOT NULL CHECK (release_weekday BETWEEN 0 AND 6),
    release_time time NOT NULL,
    release_weeks_ahead integer NOT NULL CHECK (release_weeks_ahead BETWEEN 1 AND 52),
    -- Zero means "until the start", so the cutoffs are never NULL.
    booking_cutoff interval NOT NULL CHECK (booking_cutoff BETWEEN '0' AND '30 days'),
    cancellation_cutoff interval NOT NULL CHECK (cancellation_cutoff BETWEEN '0' AND '30 days'),
    default_visible_days integer NOT NULL CHECK (default_visible_days BETWEEN 1 AND 366),
    legal_notice text CHECK (legal_notice <> '')
);

CREATE TABLE persons (
    id uuid PRIMARY KEY,
    given_name text COLLATE "C" NOT NULL CHECK (btrim(given_name) <> ''),
    family_name text COLLATE "C" NOT NULL CHECK (btrim(family_name) <> ''),
    email text CHECK (email <> ''),
    phone text CHECK (phone <> ''),
    birth_date date,
    notes text CHECK (notes <> '')
);

CREATE INDEX persons_name_idx ON persons (family_name, given_name, id);

CREATE TABLE person_guardians (
    guardian_id uuid NOT NULL REFERENCES persons ON DELETE RESTRICT,
    dependent_id uuid NOT NULL REFERENCES persons ON DELETE RESTRICT,
    PRIMARY KEY (guardian_id, dependent_id),
    CONSTRAINT person_guardians_not_self CHECK (guardian_id <> dependent_id)
);

CREATE INDEX person_guardians_dependent_idx ON person_guardians (dependent_id);

CREATE TABLE accounts (
    id uuid PRIMARY KEY,
    person_id uuid NOT NULL REFERENCES persons ON DELETE RESTRICT,
    -- Unique ignoring ASCII case only: lower() under collation "C" folds
    -- A-Z and nothing else, so "Ä" and "ä" stay different (citext would
    -- fold with the database collation). Queries compare lower(email) with
    -- lower($1 COLLATE "C") to use accounts_email_key.
    email text COLLATE "C" NOT NULL CHECK (email <> ''),
    -- No 'guest': guests have no account (domain.Account.Validate).
    role text NOT NULL CHECK (role IN ('member', 'instructor', 'office', 'admin')),
    -- NULL means "no override" (domain zero value).
    visible_days integer CHECK (visible_days BETWEEN 1 AND 366),
    disabled_at timestamptz,
    CONSTRAINT accounts_person_id_key UNIQUE (person_id)
);

CREATE UNIQUE INDEX accounts_email_key ON accounts (lower(email));

CREATE TABLE locations (
    id uuid PRIMARY KEY,
    name text COLLATE "C" NOT NULL CHECK (btrim(name) <> ''),
    address text CHECK (address <> '')
);

CREATE TABLE rooms (
    id uuid PRIMARY KEY,
    location_id uuid NOT NULL REFERENCES locations ON DELETE RESTRICT,
    name text COLLATE "C" NOT NULL CHECK (btrim(name) <> ''),
    capacity integer NOT NULL CHECK (capacity > 0)
);

CREATE INDEX rooms_location_idx ON rooms (location_id);

CREATE TABLE staff (
    id uuid PRIMARY KEY,
    person_id uuid REFERENCES persons ON DELETE RESTRICT,
    display_name text COLLATE "C" NOT NULL CHECK (btrim(display_name) <> ''),
    kind text NOT NULL CHECK (kind IN ('instructor', 'accompanist')),
    active boolean NOT NULL
);

CREATE INDEX staff_person_idx ON staff (person_id);

CREATE TABLE course_types (
    id uuid PRIMARY KEY,
    name text COLLATE "C" NOT NULL CHECK (btrim(name) <> ''),
    description text CHECK (description <> '')
);

CREATE TABLE terms (
    id uuid PRIMARY KEY,
    name text COLLATE "C" NOT NULL CHECK (btrim(name) <> ''),
    starts_on date NOT NULL,
    ends_on date NOT NULL,
    CONSTRAINT terms_dates_ordered CHECK (starts_on <= ends_on)
);

CREATE TABLE classes (
    id uuid PRIMARY KEY,
    term_id uuid NOT NULL REFERENCES terms ON DELETE RESTRICT,
    course_type_id uuid NOT NULL REFERENCES course_types ON DELETE RESTRICT,
    room_id uuid NOT NULL REFERENCES rooms ON DELETE RESTRICT,
    instructor_id uuid NOT NULL REFERENCES staff ON DELETE RESTRICT,
    accompanist_id uuid REFERENCES staff ON DELETE RESTRICT,
    level text NOT NULL CHECK (level IN ('beginner', 'intermediate', 'advanced', 'professional', 'open')),
    -- time.Weekday: 0 = Sunday.
    weekday smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    start_time time NOT NULL,
    duration interval NOT NULL CHECK (duration > '0' AND duration <= '24 hours'),
    capacity integer NOT NULL CHECK (capacity > 0),
    CONSTRAINT classes_accompanist_not_instructor CHECK (accompanist_id <> instructor_id)
);

CREATE INDEX classes_term_idx ON classes (term_id, weekday, start_time, id);
CREATE INDEX classes_course_type_idx ON classes (course_type_id);
CREATE INDEX classes_room_idx ON classes (room_id);
CREATE INDEX classes_instructor_idx ON classes (instructor_id);
CREATE INDEX classes_accompanist_idx ON classes (accompanist_id);

CREATE TABLE class_members (
    class_id uuid NOT NULL REFERENCES classes ON DELETE RESTRICT,
    person_id uuid NOT NULL REFERENCES persons ON DELETE RESTRICT,
    since date,
    PRIMARY KEY (class_id, person_id)
);

CREATE INDEX class_members_person_idx ON class_members (person_id);

CREATE TABLE lessons (
    id uuid PRIMARY KEY,
    -- NULL for one-off lessons, which are not covered by lessons_class_start_key.
    class_id uuid REFERENCES classes ON DELETE RESTRICT,
    course_type_id uuid NOT NULL REFERENCES course_types ON DELETE RESTRICT,
    room_id uuid NOT NULL REFERENCES rooms ON DELETE RESTRICT,
    instructor_id uuid NOT NULL REFERENCES staff ON DELETE RESTRICT,
    accompanist_id uuid REFERENCES staff ON DELETE RESTRICT,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('scheduled', 'canceled')),
    visibility text NOT NULL CHECK (visibility IN ('public', 'members', 'hidden')),
    kind text NOT NULL CHECK (kind IN ('regular', 'workshop', 'private')),
    capacity integer NOT NULL CHECK (capacity > 0),
    CONSTRAINT lessons_ends_after_start CHECK (ends_at > starts_at),
    CONSTRAINT lessons_accompanist_not_instructor CHECK (accompanist_id <> instructor_id),
    CONSTRAINT lessons_class_start_key UNIQUE (class_id, starts_at)
);

CREATE INDEX lessons_starts_at_idx ON lessons (starts_at, id);
CREATE INDEX lessons_course_type_idx ON lessons (course_type_id);
CREATE INDEX lessons_room_idx ON lessons (room_id);
CREATE INDEX lessons_instructor_idx ON lessons (instructor_id);
CREATE INDEX lessons_accompanist_idx ON lessons (accompanist_id);

CREATE TABLE bookings (
    id uuid PRIMARY KEY,
    lesson_id uuid NOT NULL REFERENCES lessons ON DELETE RESTRICT,
    -- NULL for guest bookings, which carry a name and an email instead.
    person_id uuid REFERENCES persons ON DELETE RESTRICT,
    guest_name text CHECK (btrim(guest_name) <> ''),
    guest_email text CHECK (guest_email <> ''),
    status text NOT NULL CHECK (status IN ('booked', 'canceled', 'attended', 'no_show')),
    trial boolean NOT NULL,
    prepaid boolean NOT NULL,
    booked_at timestamptz NOT NULL,
    canceled_at timestamptz,
    CONSTRAINT bookings_person_or_guest CHECK (
        (person_id IS NOT NULL AND guest_name IS NULL AND guest_email IS NULL)
        OR (person_id IS NULL AND guest_name IS NOT NULL AND guest_email IS NOT NULL)
    ),
    CONSTRAINT bookings_canceled_at_iff_canceled CHECK ((status = 'canceled') = (canceled_at IS NOT NULL)),
    CONSTRAINT bookings_canceled_after_booked CHECK (canceled_at >= booked_at)
);

-- A person has at most one active (not canceled) booking per lesson.
CREATE UNIQUE INDEX bookings_one_active_key ON bookings (lesson_id, person_id)
    WHERE status <> 'canceled' AND person_id IS NOT NULL;
CREATE INDEX bookings_lesson_idx ON bookings (lesson_id, booked_at, id);
CREATE INDEX bookings_person_idx ON bookings (person_id);

CREATE TABLE booking_events (
    id uuid PRIMARY KEY,
    booking_id uuid NOT NULL REFERENCES bookings ON DELETE RESTRICT,
    at timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('booked', 'canceled', 'attended', 'no_show')),
    -- NULL for system actions.
    actor_account_id uuid REFERENCES accounts ON DELETE RESTRICT,
    note text CHECK (note <> '')
);

CREATE INDEX booking_events_booking_idx ON booking_events (booking_id, at, id);
CREATE INDEX booking_events_actor_idx ON booking_events (actor_account_id);

-- +goose Down

DROP TABLE booking_events;
DROP TABLE bookings;
DROP TABLE lessons;
DROP TABLE class_members;
DROP TABLE classes;
DROP TABLE terms;
DROP TABLE course_types;
DROP TABLE staff;
DROP TABLE rooms;
DROP TABLE locations;
DROP TABLE accounts;
DROP TABLE person_guardians;
DROP TABLE persons;
DROP TABLE settings;
