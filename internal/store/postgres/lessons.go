package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// ---------------------------------------------------------------- lessons

type lessonRepo struct{ q *sqlc.Queries }

func toLesson(row sqlc.Lesson) domain.Lesson {
	return domain.Lesson{
		ID:            fromPgID[domain.LessonID](row.ID),
		ClassID:       fromPgID[domain.ClassID](row.ClassID),
		SlotDate:      fromPgDate(row.SlotDate),
		CourseTypeID:  fromPgID[domain.CourseTypeID](row.CourseTypeID),
		RoomID:        fromPgID[domain.RoomID](row.RoomID),
		InstructorID:  fromPgID[domain.StaffID](row.InstructorID),
		AccompanistID: fromPgID[domain.StaffID](row.AccompanistID),
		StartsAt:      fromPgInstant(row.StartsAt),
		EndsAt:        fromPgInstant(row.EndsAt),
		Status:        domain.LessonStatus(row.Status),
		Visibility:    domain.LessonVisibility(row.Visibility),
		Kind:          domain.LessonKind(row.Kind),
		Capacity:      int(row.Capacity),
		Detached:      row.Detached,
		CancelNote:    row.CancelNote.String,
		CanceledAt:    fromPgInstant(row.CanceledAt),
		CanceledBy:    fromPgID[domain.AccountID](row.CanceledBy),
	}
}

// lessonParams converts l to the parameters shared by insert and update.
func lessonParams(l domain.Lesson) (sqlc.CreateLessonParams, error) {
	capacity, err := int4(l.Capacity)
	if err != nil {
		return sqlc.CreateLessonParams{}, err
	}

	return sqlc.CreateLessonParams{
		ID:            pgID(l.ID),
		ClassID:       pgID(l.ClassID),
		CourseTypeID:  pgID(l.CourseTypeID),
		RoomID:        pgID(l.RoomID),
		InstructorID:  pgID(l.InstructorID),
		AccompanistID: pgID(l.AccompanistID),
		StartsAt:      pgInstant(l.StartsAt),
		EndsAt:        pgInstant(l.EndsAt),
		Status:        string(l.Status),
		Visibility:    string(l.Visibility),
		Kind:          string(l.Kind),
		Capacity:      capacity,
		SlotDate:      pgDate(l.SlotDate),
		Detached:      l.Detached,
		CancelNote:    pgText(l.CancelNote),
		CanceledAt:    pgInstant(l.CanceledAt),
		CanceledBy:    pgID(l.CanceledBy),
	}, nil
}

func (r lessonRepo) Get(ctx context.Context, id domain.LessonID) (domain.Lesson, error) {
	row, err := r.q.GetLesson(ctx, pgID(id))
	if err != nil {
		return domain.Lesson{}, notFound("lesson", err)
	}

	return toLesson(row), nil
}

func (r lessonRepo) Create(ctx context.Context, l domain.Lesson) error {
	p, err := lessonParams(l)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreateLesson(ctx, p))
}

func (r lessonRepo) Update(ctx context.Context, l domain.Lesson) error {
	p, err := lessonParams(l)
	if err != nil {
		return err
	}

	n, err := r.q.UpdateLesson(ctx, sqlc.UpdateLessonParams(p))

	return affected("lesson", n, err)
}

func (r lessonRepo) Delete(ctx context.Context, id domain.LessonID) error {
	n, err := r.q.DeleteLesson(ctx, pgID(id))

	return affected("lesson", n, err)
}

func (r lessonRepo) ListByClass(ctx context.Context, class domain.ClassID, from domain.Date) ([]domain.Lesson, error) {
	rows, err := r.q.ListLessonsByClass(ctx, sqlc.ListLessonsByClassParams{ClassID: pgID(class), FromSlot: pgDate(from)})

	return convertRows(rows, err, toLesson)
}

func (r lessonRepo) ListRange(ctx context.Context, from, to time.Time) ([]domain.Lesson, error) {
	rows, err := r.q.ListLessonsInRange(ctx, sqlc.ListLessonsInRangeParams{
		FromTime: pgtype.Timestamptz{Time: instant(from), Valid: true},
		ToTime:   pgtype.Timestamptz{Time: instant(to), Valid: true},
	})

	return convertRows(rows, err, toLesson)
}

func (r lessonRepo) ListRoomOverlaps(
	ctx context.Context, room domain.RoomID, from, to time.Time, exclude domain.LessonID,
) ([]domain.Lesson, error) {
	rows, err := r.q.ListRoomOverlaps(ctx, sqlc.ListRoomOverlapsParams{
		RoomID:    pgID(room),
		FromTime:  pgtype.Timestamptz{Time: instant(from), Valid: true},
		ToTime:    pgtype.Timestamptz{Time: instant(to), Valid: true},
		ExcludeID: pgID(exclude),
	})

	return convertRows(rows, err, toLesson)
}

// Lock reads the lesson with SELECT … FOR UPDATE. In a read-only transaction
// PostgreSQL rejects the statement (SQLSTATE 25006, store.ErrReadOnly).
func (r lessonRepo) Lock(ctx context.Context, id domain.LessonID) (domain.Lesson, error) {
	row, err := r.q.LockLesson(ctx, pgID(id))
	if err != nil {
		return domain.Lesson{}, notFound("lesson", err)
	}

	return toLesson(row), nil
}

// ---------------------------------------------------------------- bookings

type bookingRepo struct{ q *sqlc.Queries }

func toBooking(row sqlc.Booking) domain.Booking {
	return domain.Booking{
		ID:         fromPgID[domain.BookingID](row.ID),
		LessonID:   fromPgID[domain.LessonID](row.LessonID),
		PersonID:   fromPgID[domain.PersonID](row.PersonID),
		GuestName:  row.GuestName.String,
		GuestEmail: row.GuestEmail.String,
		Status:     domain.BookingStatus(row.Status),
		Trial:      row.Trial,
		Prepaid:    row.Prepaid,
		BookedAt:   fromPgInstant(row.BookedAt),
		CanceledAt: fromPgInstant(row.CanceledAt),
	}
}

func bookingParams(b domain.Booking) sqlc.CreateBookingParams {
	return sqlc.CreateBookingParams{
		ID:         pgID(b.ID),
		LessonID:   pgID(b.LessonID),
		PersonID:   pgID(b.PersonID),
		GuestName:  pgText(b.GuestName),
		GuestEmail: pgText(b.GuestEmail),
		Status:     string(b.Status),
		Trial:      b.Trial,
		Prepaid:    b.Prepaid,
		BookedAt:   pgInstant(b.BookedAt),
		CanceledAt: pgInstant(b.CanceledAt),
	}
}

func (r bookingRepo) Get(ctx context.Context, id domain.BookingID) (domain.Booking, error) {
	row, err := r.q.GetBooking(ctx, pgID(id))
	if err != nil {
		return domain.Booking{}, notFound("booking", err)
	}

	return toBooking(row), nil
}

func (r bookingRepo) Create(ctx context.Context, b domain.Booking) error {
	return mapErr(r.q.CreateBooking(ctx, bookingParams(b)))
}

func (r bookingRepo) Update(ctx context.Context, b domain.Booking) error {
	n, err := r.q.UpdateBooking(ctx, sqlc.UpdateBookingParams(bookingParams(b)))

	return affected("booking", n, err)
}

func (r bookingRepo) ListByLesson(ctx context.Context, lesson domain.LessonID) ([]domain.Booking, error) {
	rows, err := r.q.ListBookingsByLesson(ctx, pgID(lesson))

	return convertRows(rows, err, toBooking)
}

func (r bookingRepo) CountActive(ctx context.Context, lesson domain.LessonID) (int, error) {
	n, err := r.q.CountActiveBookings(ctx, pgID(lesson))
	if err != nil {
		return 0, mapErr(err)
	}

	return int(n), nil
}

func (r bookingRepo) AddEvent(ctx context.Context, e domain.BookingEvent) error {
	return mapErr(r.q.AddBookingEvent(ctx, sqlc.AddBookingEventParams{
		ID:             pgID(e.ID),
		BookingID:      pgID(e.BookingID),
		At:             pgInstant(e.At),
		Status:         string(e.Status),
		ActorAccountID: pgID(e.ActorAccountID),
		Note:           pgText(e.Note),
	}))
}

func (r bookingRepo) ListEvents(ctx context.Context, booking domain.BookingID) ([]domain.BookingEvent, error) {
	rows, err := r.q.ListBookingEvents(ctx, pgID(booking))

	return convertRows(rows, err, func(row sqlc.BookingEvent) domain.BookingEvent {
		return domain.BookingEvent{
			ID:             fromPgID[domain.BookingEventID](row.ID),
			BookingID:      fromPgID[domain.BookingID](row.BookingID),
			At:             fromPgInstant(row.At),
			Status:         domain.BookingStatus(row.Status),
			ActorAccountID: fromPgID[domain.AccountID](row.ActorAccountID),
			Note:           row.Note.String,
		}
	})
}
