package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Constraints and unique indexes whose violation has a domain meaning beyond
// "duplicate key" (see migrations/00001_core.sql, 00006, 00007, 00008, 00012,
// 00015, 00016, 00017 and 00021).
const (
	constraintMemberNo        = "persons_member_no_key"
	constraintAccountEmail    = "accounts_email_key"
	constraintAccountPerson   = "accounts_person_id_key"
	constraintGuardianLink    = "person_guardians_pkey"
	constraintClassMember     = "class_members_pkey"
	constraintLessonClassSlot = "lessons_class_slot_key"
	constraintActiveBooking   = "bookings_one_active_key"
	constraintLocationName    = "locations_name_key"
	constraintRoomName        = "rooms_location_name_key"
	constraintCourseTypeName  = "course_types_name_key"
	constraintOpenWaitlist    = "waitlist_entries_one_open_key"
	constraintActiveFeed      = "calendar_feeds_one_active_key"
	constraintPassTypeName    = "pass_types_name_key"
	constraintRedemption      = "pass_redemptions_booking_key"
	constraintRedemptionKey   = "pass_redemptions_pkey"
	constraintReceiptNumber   = "receipts_number_key"
	constraintReceiptSequence = "receipts_year_sequence_key"
	constraintReceiptCancels  = "receipts_cancels_key"
	constraintReceiptPDF      = "receipt_pdfs_pkey"
)

// conflicts describes the unique constraints above: the message, and the
// error it wraps (domain.ErrDuplicate for the unique names of ADR 0007 and
// for what exists only once per person: an account, a guardian link, a
// class membership, an active booking of a lesson, an active calendar feed
// of a subject, a pass type's name, a booking's redemption, a receipt's
// number, cancellation and PDF, a person's member number).
// Other unique and foreign key violations get a generic message naming the
// constraint.
var conflicts = map[string]struct {
	msg string
	err error
}{
	constraintMemberNo:        {"member number is taken", domain.ErrDuplicate},
	constraintAccountEmail:    {"account email is taken", domain.ErrDuplicate},
	constraintAccountPerson:   {"person already has an account", domain.ErrDuplicate},
	constraintGuardianLink:    {"guardian link exists", domain.ErrDuplicate},
	constraintClassMember:     {"person is already a member of the class", domain.ErrDuplicate},
	constraintLessonClassSlot: {"class already has a lesson in this slot", domain.ErrConflict},
	constraintActiveBooking:   {"person already has an active booking for the lesson", domain.ErrDuplicate},
	constraintLocationName:    {"location name is taken", domain.ErrDuplicate},
	constraintRoomName:        {"room name is taken at the location", domain.ErrDuplicate},
	constraintCourseTypeName:  {"course type name is taken", domain.ErrDuplicate},
	constraintOpenWaitlist:    {"person is on the waiting list of the lesson already", domain.ErrDuplicate},
	constraintActiveFeed:      {"the account has an active calendar feed of this subject", domain.ErrDuplicate},
	constraintPassTypeName:    {"pass type name is taken", domain.ErrDuplicate},
	constraintRedemption:      {"a pass paid for the booking already", domain.ErrDuplicate},
	constraintRedemptionKey:   {"a pass paid for the booking already", domain.ErrDuplicate},
	constraintReceiptNumber:   {"the receipt number is taken", domain.ErrDuplicate},
	constraintReceiptSequence: {"the receipt sequence number is taken", domain.ErrDuplicate},
	constraintReceiptCancels:  {"the receipt is canceled already", domain.ErrDuplicate},
	constraintReceiptPDF:      {"the receipt has its PDF already", domain.ErrDuplicate},
}

// retryableError reports a serialization failure or deadlock. InTx retries
// the transaction; if it gives up, the error reports a conflict, because
// the transaction lost against concurrent ones.
type retryableError struct {
	code string
}

func (e *retryableError) Error() string {
	if e.code == pgerrcode.DeadlockDetected {
		return "postgres: deadlock detected"
	}

	return "postgres: serialization failure"
}

// lostRace is the error InTx returns when it gives up after the retryable
// failure err: a conflict, because the transaction kept losing against
// concurrent ones. Only here does a retryable failure become ErrConflict;
// inside the callback it must stay distinct, or a callback that handles
// ErrConflict would swallow it and prevent the retry. A raw driver error that
// a callback returned never leaves the package.
func lostRace(err error, attempts int) error {
	reason := "serialization failure"

	var (
		re    *retryableError
		pgErr *pgconn.PgError
	)

	if (errors.As(err, &re) && re.code == pgerrcode.DeadlockDetected) ||
		(errors.As(err, &pgErr) && pgErr.Code == pgerrcode.DeadlockDetected) {
		reason = "deadlock detected"
	}

	return fmt.Errorf("%w: postgres: %s, giving up after %d attempts", domain.ErrConflict, reason, attempts)
}

// retryable reports whether the transaction that failed with err may succeed
// when run again. It also recognizes a driver error that a callback returned
// unmapped.
func retryable(err error) bool {
	var re *retryableError
	if errors.As(err, &re) {
		return true
	}

	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && isRetryableCode(pgErr.Code)
}

func isRetryableCode(code string) bool {
	return code == pgerrcode.SerializationFailure || code == pgerrcode.DeadlockDetected
}

// mapErr translates a driver error into the store's error vocabulary:
// domain.ErrNotFound, domain.ErrConflict, domain.ErrValidation,
// store.ErrReadOnly, context errors, or an opaque error. The result never
// wraps a driver error, and messages contain no values (an email in a
// duplicate key, say), only constraint names.
func mapErr(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return mapPgError(pgErr)
	}

	// Canceled requests keep their cause, so callers can tell them apart.
	for _, ctxErr := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, ctxErr) {
			return fmt.Errorf("postgres: %w", ctxErr)
		}
	}

	return errors.New("postgres: " + err.Error())
}

func mapPgError(e *pgconn.PgError) error {
	switch {
	case e.Code == pgerrcode.UniqueViolation || e.Code == pgerrcode.ExclusionViolation:
		if c, ok := conflicts[e.ConstraintName]; ok {
			return fmt.Errorf("%s: %w", c.msg, c.err)
		}

		return fmt.Errorf("duplicate key (%s): %w", e.ConstraintName, domain.ErrConflict)
	case e.Code == pgerrcode.ForeignKeyViolation || e.Code == pgerrcode.RestrictViolation:
		// A reference to a missing record (23503), or the deletion of a
		// referenced one (23001 for ON DELETE RESTRICT); both are conflicts.
		return fmt.Errorf("foreign key %s violated: %w", e.ConstraintName, domain.ErrConflict)
	case e.Code == pgerrcode.ReadOnlySQLTransaction:
		return store.ErrReadOnly
	case isRetryableCode(e.Code):
		return &retryableError{code: e.Code}
	case e.Code == pgerrcode.CheckViolation || e.Code == pgerrcode.NotNullViolation:
		// Services validate before writing, so this is a bug in a service
		// (or a Validate method laxer than the schema). These messages name
		// the constraint or column, never the value.
		return fmt.Errorf("postgres: %s: %w", e.Message, domain.ErrValidation)
	case pgerrcode.IsDataException(e.Code):
		// Messages of data exceptions may quote the value, so they are left out.
		return fmt.Errorf("postgres: invalid value (SQLSTATE %s): %w", e.Code, domain.ErrValidation)
	default:
		return fmt.Errorf("postgres: %s (SQLSTATE %s)", strings.TrimSpace(e.Message), e.Code)
	}
}

// notFound maps err like mapErr and names the missing record.
func notFound(what string, err error) error {
	err = mapErr(err)
	if errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("%s: %w", what, domain.ErrNotFound)
	}

	return err
}

// affected turns the row count of an UPDATE or DELETE into an error:
// domain.ErrNotFound if no row matched.
func affected(what string, n int64, err error) error {
	if err != nil {
		return mapErr(err)
	}

	if n == 0 {
		return fmt.Errorf("%s: %w", what, domain.ErrNotFound)
	}

	return nil
}
