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
// "duplicate key" (see migrations/00001_core.sql).
const (
	constraintAccountEmail     = "accounts_email_key"
	constraintAccountPerson    = "accounts_person_id_key"
	constraintGuardianLink     = "person_guardians_pkey"
	constraintClassMember      = "class_members_pkey"
	constraintLessonClassStart = "lessons_class_start_key"
	constraintActiveBooking    = "bookings_one_active_key"
)

// conflictMessages describes the unique constraints above. Other unique and
// foreign key violations get a generic message naming the constraint.
var conflictMessages = map[string]string{
	constraintAccountEmail:     "account email is taken",
	constraintAccountPerson:    "person already has an account",
	constraintGuardianLink:     "guardian link exists",
	constraintClassMember:      "person is already a member of the class",
	constraintLessonClassStart: "class already has a lesson at this start",
	constraintActiveBooking:    "person already has an active booking for the lesson",
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
		if msg, ok := conflictMessages[e.ConstraintName]; ok {
			return fmt.Errorf("%s: %w", msg, domain.ErrConflict)
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
