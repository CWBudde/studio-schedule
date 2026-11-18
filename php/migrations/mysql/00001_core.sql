-- Core aggregates: settings, people, catalog, timetable, lessons, bookings.
--
-- UTC instants use DATETIME(6), wall times use TIME(6), and durations use
-- integer microseconds. UUIDs are stored in their 16-byte order.
-- User text uses utf8mb4 with binary collation. Enums and unique comparison
-- keys are binary strings to preserve bytewise equality and trailing spaces.
-- Foreign keys restrict deletes unless a migration specifies otherwise.

-- +goose Up

CREATE TABLE settings (
    -- A single row: the key can only be true.
    id BOOLEAN PRIMARY KEY DEFAULT TRUE,
    school_name LONGTEXT NOT NULL,
    timezone LONGTEXT NOT NULL,
    default_locale LONGTEXT NOT NULL,
    currency VARBINARY(3) NOT NULL,
    receipt_prefix VARBINARY(16) NOT NULL,
    release_weekday SMALLINT NOT NULL,
    release_time TIME(6) NOT NULL,
    CONSTRAINT settings_release_time_range CHECK (release_time BETWEEN '00:00:00' AND '24:00:00'),
    release_weeks_ahead INT NOT NULL,
    -- Zero means "until the start", so the cutoffs are never NULL.
    booking_cutoff BIGINT NOT NULL,
    cancellation_cutoff BIGINT NOT NULL,
    default_visible_days INT NOT NULL,
    legal_notice LONGTEXT,
    CONSTRAINT settings_singleton CHECK (id = TRUE),
    CONSTRAINT settings_school_name_check CHECK (OCTET_LENGTH(TRIM(school_name)) > 0),
    CONSTRAINT settings_timezone_check CHECK (OCTET_LENGTH(timezone) > 0),
    CONSTRAINT settings_default_locale_check CHECK (OCTET_LENGTH(TRIM(default_locale)) > 0),
    CONSTRAINT settings_currency_check CHECK (
        OCTET_LENGTH(currency) = 3
        AND CONVERT(currency USING utf8mb4) COLLATE utf8mb4_bin NOT REGEXP '[^A-Z]'
    ),
    CONSTRAINT settings_receipt_prefix_check CHECK (
        OCTET_LENGTH(receipt_prefix) > 0
        AND CONVERT(receipt_prefix USING utf8mb4) COLLATE utf8mb4_bin NOT REGEXP '[^A-Z0-9]'
    ),
    CONSTRAINT settings_release_weekday_check CHECK (release_weekday BETWEEN 0 AND 6),
    CONSTRAINT settings_release_weeks_ahead_check CHECK (release_weeks_ahead BETWEEN 1 AND 52),
    CONSTRAINT settings_booking_cutoff_check CHECK (booking_cutoff BETWEEN 0 AND 2592000000000),
    CONSTRAINT settings_cancellation_cutoff_check CHECK (cancellation_cutoff BETWEEN 0 AND 2592000000000),
    CONSTRAINT settings_default_visible_days_check CHECK (default_visible_days BETWEEN 1 AND 366),
    CONSTRAINT settings_legal_notice_check CHECK (OCTET_LENGTH(legal_notice) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE persons (
    id BINARY(16) PRIMARY KEY,
    given_name LONGTEXT NOT NULL,
    family_name LONGTEXT NOT NULL,
    email LONGTEXT,
    phone LONGTEXT,
    birth_date DATE,
    notes LONGTEXT,
    CONSTRAINT persons_given_name_check CHECK (OCTET_LENGTH(TRIM(given_name)) > 0),
    CONSTRAINT persons_family_name_check CHECK (OCTET_LENGTH(TRIM(family_name)) > 0),
    CONSTRAINT persons_email_check CHECK (OCTET_LENGTH(email) > 0),
    CONSTRAINT persons_phone_check CHECK (OCTET_LENGTH(phone) > 0),
    CONSTRAINT persons_notes_check CHECK (OCTET_LENGTH(notes) > 0),
    KEY persons_name_idx (family_name(255), given_name(255), id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE person_guardians (
    guardian_id BINARY(16) NOT NULL,
    dependent_id BINARY(16) NOT NULL,
    PRIMARY KEY (guardian_id, dependent_id),
    CONSTRAINT person_guardians_not_self CHECK (guardian_id <> dependent_id),
    KEY person_guardians_dependent_idx (dependent_id),
    CONSTRAINT person_guardians_guardian_id_fkey FOREIGN KEY (guardian_id) REFERENCES persons(id) ON DELETE RESTRICT,
    CONSTRAINT person_guardians_dependent_id_fkey FOREIGN KEY (dependent_id) REFERENCES persons(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE accounts (
    id BINARY(16) PRIMARY KEY,
    person_id BINARY(16) NOT NULL,
    email LONGTEXT NOT NULL,
    -- Binary REPLACE folds only ASCII A-Z, matching PostgreSQL lower() under C collation.
    -- The 3072-byte key limit is above the PostgreSQL B-tree key limit.
    email_folded VARBINARY(3072) GENERATED ALWAYS AS (
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
        REPLACE(REPLACE(CAST(email AS BINARY), 'A', 'a'), 'B', 'b'), 'C', 'c'), 'D', 'd'), 'E', 'e'), 'F', 'f'),
        'G', 'g'), 'H', 'h'), 'I', 'i'), 'J', 'j'), 'K', 'k'), 'L', 'l'),
        'M', 'm'), 'N', 'n'), 'O', 'o'), 'P', 'p'), 'Q', 'q'), 'R', 'r'),
        'S', 's'), 'T', 't'), 'U', 'u'), 'V', 'v'), 'W', 'w'), 'X', 'x'),
        'Y', 'y'), 'Z', 'z')
    ) VIRTUAL,
    -- No 'guest': guests have no account (domain.Account.Validate).
    role VARBINARY(32) NOT NULL,
    -- NULL means "no override" (domain zero value).
    visible_days INT,
    disabled_at DATETIME(6),
    CONSTRAINT accounts_person_id_key UNIQUE (person_id),
    CONSTRAINT accounts_email_check CHECK (OCTET_LENGTH(email) > 0),
    CONSTRAINT accounts_role_check CHECK (role IN ('member', 'instructor', 'office', 'admin')),
    CONSTRAINT accounts_visible_days_check CHECK (visible_days BETWEEN 1 AND 366),
    UNIQUE KEY accounts_email_key (email_folded),
    CONSTRAINT accounts_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE locations (
    id BINARY(16) PRIMARY KEY,
    name LONGTEXT NOT NULL,
    address LONGTEXT,
    CONSTRAINT locations_name_check CHECK (OCTET_LENGTH(TRIM(name)) > 0),
    CONSTRAINT locations_address_check CHECK (OCTET_LENGTH(address) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE rooms (
    id BINARY(16) PRIMARY KEY,
    location_id BINARY(16) NOT NULL,
    name LONGTEXT NOT NULL,
    capacity INT NOT NULL,
    CONSTRAINT rooms_name_check CHECK (OCTET_LENGTH(TRIM(name)) > 0),
    CONSTRAINT rooms_capacity_check CHECK (capacity > 0),
    KEY rooms_location_idx (location_id),
    CONSTRAINT rooms_location_id_fkey FOREIGN KEY (location_id) REFERENCES locations(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE staff (
    id BINARY(16) PRIMARY KEY,
    person_id BINARY(16),
    display_name LONGTEXT NOT NULL,
    kind VARBINARY(32) NOT NULL,
    active BOOLEAN NOT NULL,
    CONSTRAINT staff_display_name_check CHECK (OCTET_LENGTH(TRIM(display_name)) > 0),
    CONSTRAINT staff_kind_check CHECK (kind IN ('instructor', 'accompanist')),
    KEY staff_person_idx (person_id),
    CONSTRAINT staff_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE course_types (
    id BINARY(16) PRIMARY KEY,
    name LONGTEXT NOT NULL,
    description LONGTEXT,
    CONSTRAINT course_types_name_check CHECK (OCTET_LENGTH(TRIM(name)) > 0),
    CONSTRAINT course_types_description_check CHECK (OCTET_LENGTH(description) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE terms (
    id BINARY(16) PRIMARY KEY,
    name LONGTEXT NOT NULL,
    starts_on DATE NOT NULL,
    ends_on DATE NOT NULL,
    CONSTRAINT terms_dates_ordered CHECK (starts_on <= ends_on),
    CONSTRAINT terms_name_check CHECK (OCTET_LENGTH(TRIM(name)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE classes (
    id BINARY(16) PRIMARY KEY,
    term_id BINARY(16) NOT NULL,
    course_type_id BINARY(16) NOT NULL,
    room_id BINARY(16) NOT NULL,
    instructor_id BINARY(16) NOT NULL,
    accompanist_id BINARY(16),
    level VARBINARY(32) NOT NULL,
    -- time.Weekday: 0 = Sunday.
    weekday SMALLINT NOT NULL,
    start_time TIME(6) NOT NULL,
    CONSTRAINT classes_start_time_range CHECK (start_time BETWEEN '00:00:00' AND '24:00:00'),
    duration BIGINT NOT NULL,
    capacity INT NOT NULL,
    CONSTRAINT classes_accompanist_not_instructor CHECK (accompanist_id <> instructor_id),
    CONSTRAINT classes_level_check CHECK (level IN ('beginner', 'intermediate', 'advanced', 'professional', 'open')),
    CONSTRAINT classes_weekday_check CHECK (weekday BETWEEN 0 AND 6),
    CONSTRAINT classes_duration_check CHECK (duration > 0 AND duration <= 86400000000),
    CONSTRAINT classes_capacity_check CHECK (capacity > 0),
    KEY classes_term_idx (term_id, weekday, start_time, id),
    KEY classes_course_type_idx (course_type_id),
    KEY classes_room_idx (room_id),
    KEY classes_instructor_idx (instructor_id),
    KEY classes_accompanist_idx (accompanist_id),
    CONSTRAINT classes_term_id_fkey FOREIGN KEY (term_id) REFERENCES terms(id) ON DELETE RESTRICT,
    CONSTRAINT classes_course_type_id_fkey FOREIGN KEY (course_type_id) REFERENCES course_types(id) ON DELETE RESTRICT,
    CONSTRAINT classes_room_id_fkey FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE RESTRICT,
    CONSTRAINT classes_instructor_id_fkey FOREIGN KEY (instructor_id) REFERENCES staff(id) ON DELETE RESTRICT,
    CONSTRAINT classes_accompanist_id_fkey FOREIGN KEY (accompanist_id) REFERENCES staff(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE class_members (
    class_id BINARY(16) NOT NULL,
    person_id BINARY(16) NOT NULL,
    since DATE,
    PRIMARY KEY (class_id, person_id),
    KEY class_members_person_idx (person_id),
    CONSTRAINT class_members_class_id_fkey FOREIGN KEY (class_id) REFERENCES classes(id) ON DELETE RESTRICT,
    CONSTRAINT class_members_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE lessons (
    id BINARY(16) PRIMARY KEY,
    -- NULL for one-off lessons, which are not covered by lessons_class_start_key.
    class_id BINARY(16),
    course_type_id BINARY(16) NOT NULL,
    room_id BINARY(16) NOT NULL,
    instructor_id BINARY(16) NOT NULL,
    accompanist_id BINARY(16),
    starts_at DATETIME(6) NOT NULL,
    ends_at DATETIME(6) NOT NULL,
    status VARBINARY(32) NOT NULL,
    visibility VARBINARY(32) NOT NULL,
    kind VARBINARY(32) NOT NULL,
    capacity INT NOT NULL,
    CONSTRAINT lessons_ends_after_start CHECK (ends_at > starts_at),
    CONSTRAINT lessons_accompanist_not_instructor CHECK (accompanist_id <> instructor_id),
    CONSTRAINT lessons_class_start_key UNIQUE (class_id, starts_at),
    CONSTRAINT lessons_status_check CHECK (status IN ('scheduled', 'canceled')),
    CONSTRAINT lessons_visibility_check CHECK (visibility IN ('public', 'members', 'hidden')),
    CONSTRAINT lessons_kind_check CHECK (kind IN ('regular', 'workshop', 'private')),
    CONSTRAINT lessons_capacity_check CHECK (capacity > 0),
    KEY lessons_starts_at_idx (starts_at, id),
    KEY lessons_course_type_idx (course_type_id),
    KEY lessons_room_idx (room_id),
    KEY lessons_instructor_idx (instructor_id),
    KEY lessons_accompanist_idx (accompanist_id),
    CONSTRAINT lessons_class_id_fkey FOREIGN KEY (class_id) REFERENCES classes(id) ON DELETE RESTRICT,
    CONSTRAINT lessons_course_type_id_fkey FOREIGN KEY (course_type_id) REFERENCES course_types(id) ON DELETE RESTRICT,
    CONSTRAINT lessons_room_id_fkey FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE RESTRICT,
    CONSTRAINT lessons_instructor_id_fkey FOREIGN KEY (instructor_id) REFERENCES staff(id) ON DELETE RESTRICT,
    CONSTRAINT lessons_accompanist_id_fkey FOREIGN KEY (accompanist_id) REFERENCES staff(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE bookings (
    id BINARY(16) PRIMARY KEY,
    lesson_id BINARY(16) NOT NULL,
    -- NULL for guest bookings, which carry a name and an email instead.
    person_id BINARY(16),
    guest_name LONGTEXT,
    guest_email LONGTEXT,
    status VARBINARY(32) NOT NULL,
    trial BOOLEAN NOT NULL,
    prepaid BOOLEAN NOT NULL,
    booked_at DATETIME(6) NOT NULL,
    canceled_at DATETIME(6),
    -- Inactive and guest bookings generate NULL, which is exempt from uniqueness.
    active_person_id BINARY(16) GENERATED ALWAYS AS (
        CASE WHEN status <> 'canceled' THEN person_id END
    ) VIRTUAL,
    CONSTRAINT bookings_person_or_guest CHECK (
        (person_id IS NOT NULL AND guest_name IS NULL AND guest_email IS NULL)
        OR (person_id IS NULL AND guest_name IS NOT NULL AND guest_email IS NOT NULL)
    ),
    CONSTRAINT bookings_canceled_at_iff_canceled CHECK ((status = 'canceled') = (canceled_at IS NOT NULL)),
    CONSTRAINT bookings_canceled_after_booked CHECK (canceled_at >= booked_at),
    CONSTRAINT bookings_guest_name_check CHECK (OCTET_LENGTH(TRIM(guest_name)) > 0),
    CONSTRAINT bookings_guest_email_check CHECK (OCTET_LENGTH(guest_email) > 0),
    CONSTRAINT bookings_status_check CHECK (status IN ('booked', 'canceled', 'attended', 'no_show')),
    UNIQUE KEY bookings_one_active_key (lesson_id, active_person_id),
    KEY bookings_lesson_idx (lesson_id, booked_at, id),
    KEY bookings_person_idx (person_id),
    CONSTRAINT bookings_lesson_id_fkey FOREIGN KEY (lesson_id) REFERENCES lessons(id) ON DELETE RESTRICT,
    CONSTRAINT bookings_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE booking_events (
    id BINARY(16) PRIMARY KEY,
    booking_id BINARY(16) NOT NULL,
    at DATETIME(6) NOT NULL,
    status VARBINARY(32) NOT NULL,
    -- NULL for system actions.
    actor_account_id BINARY(16),
    note LONGTEXT,
    CONSTRAINT booking_events_status_check CHECK (status IN ('booked', 'canceled', 'attended', 'no_show')),
    CONSTRAINT booking_events_note_check CHECK (OCTET_LENGTH(note) > 0),
    KEY booking_events_booking_idx (booking_id, at, id),
    KEY booking_events_actor_idx (actor_account_id),
    CONSTRAINT booking_events_booking_id_fkey FOREIGN KEY (booking_id) REFERENCES bookings(id) ON DELETE RESTRICT,
    CONSTRAINT booking_events_actor_account_id_fkey FOREIGN KEY (actor_account_id) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

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
