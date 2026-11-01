package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// ---------------------------------------------------------------- events

type eventRepo struct{ q *sqlc.Queries }

func toEvent(row sqlc.Event) domain.Event {
	return domain.Event{
		ID:             fromPgID[domain.EventID](row.ID),
		Slug:           row.Slug,
		Title:          row.Title,
		Description:    row.Description.String,
		CourseTypeID:   fromPgID[domain.CourseTypeID](row.CourseTypeID),
		Capacity:       int(row.Capacity),
		Price:          domain.Money(row.Price),
		VATRate:        int(row.VatRateBp),
		Status:         domain.EventStatus(row.Status),
		SignupOpensAt:  fromPgInstant(row.SignupOpensAt),
		SignupClosesAt: fromPgInstant(row.SignupClosesAt),
		CancelUntil:    fromPgInstant(row.CancelUntil),
		CreatedAt:      fromPgInstant(row.CreatedAt),
		UpdatedAt:      fromPgInstant(row.UpdatedAt),
		CreatedBy:      fromPgID[domain.AccountID](row.CreatedBy),
	}
}

// eventParams converts e to the parameters shared by insert and update.
func eventParams(e domain.Event) (sqlc.CreateEventParams, error) {
	capacity, err := int4(e.Capacity)
	if err != nil {
		return sqlc.CreateEventParams{}, err
	}

	vat, err := int4(e.VATRate)
	if err != nil {
		return sqlc.CreateEventParams{}, err
	}

	return sqlc.CreateEventParams{
		ID:             pgID(e.ID),
		Slug:           e.Slug,
		Title:          e.Title,
		Description:    pgText(e.Description),
		CourseTypeID:   pgID(e.CourseTypeID),
		Capacity:       capacity,
		Price:          int64(e.Price),
		VatRateBp:      vat,
		Status:         string(e.Status),
		SignupOpensAt:  pgInstant(e.SignupOpensAt),
		SignupClosesAt: pgInstant(e.SignupClosesAt),
		CancelUntil:    pgInstant(e.CancelUntil),
		CreatedAt:      pgInstant(e.CreatedAt),
		UpdatedAt:      pgInstant(e.UpdatedAt),
		CreatedBy:      pgID(e.CreatedBy),
	}, nil
}

func (r eventRepo) Get(ctx context.Context, id domain.EventID) (domain.Event, error) {
	row, err := r.q.GetEvent(ctx, pgID(id))
	if err != nil {
		return domain.Event{}, notFound("event", err)
	}

	return toEvent(row), nil
}

func (r eventRepo) BySlug(ctx context.Context, slug string) (domain.Event, error) {
	row, err := r.q.GetEventBySlug(ctx, slug)
	if err != nil {
		return domain.Event{}, notFound("event", err)
	}

	return toEvent(row), nil
}

func (r eventRepo) Create(ctx context.Context, e domain.Event) error {
	p, err := eventParams(e)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreateEvent(ctx, p))
}

func (r eventRepo) Update(ctx context.Context, e domain.Event) error {
	p, err := eventParams(e)
	if err != nil {
		return err
	}

	n, err := r.q.UpdateEvent(ctx, sqlc.UpdateEventParams(p))

	return affected("event", n, err)
}

// Delete deletes the event; its image goes with it (ON DELETE CASCADE). A
// session or registration referring to it fails the statement (ON DELETE
// RESTRICT), which deletes nothing.
func (r eventRepo) Delete(ctx context.Context, id domain.EventID) error {
	n, err := r.q.DeleteEvent(ctx, pgID(id))

	err = affected("event", n, err)
	if errors.Is(err, domain.ErrConflict) {
		return fmt.Errorf("%w (%w)", domain.ErrInUse, err)
	}

	return err
}

func (r eventRepo) List(ctx context.Context) ([]domain.Event, error) {
	rows, err := r.q.ListEvents(ctx)

	return convertRows(rows, err, toEvent)
}

// Lock reads the event with SELECT … FOR UPDATE. In a read-only transaction
// PostgreSQL rejects the statement (SQLSTATE 25006, store.ErrReadOnly).
func (r eventRepo) Lock(ctx context.Context, id domain.EventID) (domain.Event, error) {
	row, err := r.q.LockEvent(ctx, pgID(id))
	if err != nil {
		return domain.Event{}, notFound("event", err)
	}

	return toEvent(row), nil
}

// Image reads the event left joined with its image: no row means no event,
// NULLs mean no image.
func (r eventRepo) Image(ctx context.Context, id domain.EventID) (domain.Background, error) {
	row, err := r.q.GetEventImage(ctx, pgID(id))
	if err != nil {
		return domain.Background{}, notFound("event", err)
	}

	if row.Image == nil {
		return domain.Background{}, nil
	}

	return domain.Background{ContentType: row.ImageType.String, Data: row.Image}, nil
}

func (r eventRepo) ImageInfo(ctx context.Context, id domain.EventID) (domain.BackgroundInfo, error) {
	row, err := r.q.GetEventImageInfo(ctx, pgID(id))
	if err != nil {
		return domain.BackgroundInfo{}, notFound("event", err)
	}

	return domain.BackgroundInfo{ContentType: row.ImageType.String, Digest: row.ImageDigest.String}, nil
}

func (r eventRepo) PutImage(ctx context.Context, id domain.EventID, img domain.Background) error {
	if img.IsZero() {
		found, err := r.q.DeleteEventImage(ctx, pgID(id))
		if err != nil {
			return mapErr(err)
		}

		if !found {
			return fmt.Errorf("event: %w", domain.ErrNotFound)
		}

		return nil
	}

	n, err := r.q.PutEventImage(ctx, sqlc.PutEventImageParams{
		EventID: pgID(id), Image: img.Data, ImageType: img.ContentType, ImageDigest: img.Digest(),
	})

	return affected("event", n, err)
}

// ---------------------------------------------------------------- registrations

type eventRegistrationRepo struct{ q *sqlc.Queries }

func toEventRegistration(row sqlc.EventRegistration) domain.EventRegistration {
	return domain.EventRegistration{
		ID:                    fromPgID[domain.EventRegistrationID](row.ID),
		EventID:               fromPgID[domain.EventID](row.EventID),
		PersonID:              fromPgID[domain.PersonID](row.PersonID),
		GuestName:             row.GuestName.String,
		GuestEmail:            row.GuestEmail.String,
		Status:                domain.EventRegistrationStatus(row.Status),
		Payment:               domain.EventPayment(row.Payment),
		Price:                 domain.Money(row.Price),
		VATRate:               int(row.VatRateBp),
		ReceiptID:             fromPgID[domain.ReceiptID](row.ReceiptID),
		CancellationReceiptID: fromPgID[domain.ReceiptID](row.CancellationReceiptID),
		InviteTokenHash:       fromPgOptHash(row.InviteTokenHash),
		InvitedAt:             fromPgInstant(row.InvitedAt),
		ExpiresAt:             fromPgInstant(row.ExpiresAt),
		RegisteredBy:          fromPgID[domain.AccountID](row.RegisteredBy),
		CreatedAt:             fromPgInstant(row.CreatedAt),
		UpdatedAt:             fromPgInstant(row.UpdatedAt),
		CanceledAt:            fromPgInstant(row.CanceledAt),
	}
}

// eventRegistrationParams converts r to the parameters shared by insert and
// update.
func eventRegistrationParams(r domain.EventRegistration) (sqlc.CreateEventRegistrationParams, error) {
	vat, err := int4(r.VATRate)
	if err != nil {
		return sqlc.CreateEventRegistrationParams{}, err
	}

	return sqlc.CreateEventRegistrationParams{
		ID:                    pgID(r.ID),
		EventID:               pgID(r.EventID),
		PersonID:              pgID(r.PersonID),
		GuestName:             pgText(r.GuestName),
		GuestEmail:            pgText(r.GuestEmail),
		Status:                string(r.Status),
		Payment:               string(r.Payment),
		Price:                 int64(r.Price),
		VatRateBp:             vat,
		ReceiptID:             pgID(r.ReceiptID),
		CancellationReceiptID: pgID(r.CancellationReceiptID),
		InviteTokenHash:       pgHash(r.InviteTokenHash),
		InvitedAt:             pgInstant(r.InvitedAt),
		ExpiresAt:             pgInstant(r.ExpiresAt),
		RegisteredBy:          pgID(r.RegisteredBy),
		CreatedAt:             pgInstant(r.CreatedAt),
		UpdatedAt:             pgInstant(r.UpdatedAt),
		CanceledAt:            pgInstant(r.CanceledAt),
	}, nil
}

func (r eventRegistrationRepo) Get(
	ctx context.Context, id domain.EventRegistrationID,
) (domain.EventRegistration, error) {
	row, err := r.q.GetEventRegistration(ctx, pgID(id))
	if err != nil {
		return domain.EventRegistration{}, notFound("event registration", err)
	}

	return toEventRegistration(row), nil
}

func (r eventRegistrationRepo) GetByInviteTokenHash(
	ctx context.Context, hash [32]byte,
) (domain.EventRegistration, error) {
	// Only an invited registration stores a hash, so the zero hash finds
	// nothing.
	row, err := r.q.GetEventRegistrationByToken(ctx, hash[:])
	if err != nil {
		return domain.EventRegistration{}, notFound("event registration", err)
	}

	return toEventRegistration(row), nil
}

func (r eventRegistrationRepo) GetByReceipt(
	ctx context.Context, receipt domain.ReceiptID,
) (domain.EventRegistration, error) {
	row, err := r.q.GetEventRegistrationByReceipt(ctx, pgID(receipt))
	if err != nil {
		return domain.EventRegistration{}, notFound("event registration", err)
	}

	return toEventRegistration(row), nil
}

func (r eventRegistrationRepo) Create(ctx context.Context, reg domain.EventRegistration) error {
	p, err := eventRegistrationParams(reg)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreateEventRegistration(ctx, p))
}

func (r eventRegistrationRepo) Update(ctx context.Context, reg domain.EventRegistration) error {
	p, err := eventRegistrationParams(reg)
	if err != nil {
		return err
	}

	n, err := r.q.UpdateEventRegistration(ctx, sqlc.UpdateEventRegistrationParams(p))

	return affected("event registration", n, err)
}

func (r eventRegistrationRepo) ListByEvent(
	ctx context.Context, event domain.EventID,
) ([]domain.EventRegistration, error) {
	rows, err := r.q.ListEventRegistrationsByEvent(ctx, pgID(event))

	return convertRows(rows, err, toEventRegistration)
}

func (r eventRegistrationRepo) ListByPersons(
	ctx context.Context, persons []domain.PersonID,
) ([]domain.EventRegistration, error) {
	rows, err := r.q.ListEventRegistrationsByPersons(ctx, pgIDs(persons))

	return convertRows(rows, err, toEventRegistration)
}

func (r eventRegistrationRepo) CountsByEvents(
	ctx context.Context, events []domain.EventID, at time.Time,
) (map[domain.EventID]store.EventRegistrationCounts, error) {
	rows, err := r.q.CountEventRegistrationsByEvents(ctx, sqlc.CountEventRegistrationsByEventsParams{
		At: pgtype.Timestamptz{Time: instant(at), Valid: true}, Events: pgIDs(events),
	})
	if err != nil {
		return nil, mapErr(err)
	}

	out := make(map[domain.EventID]store.EventRegistrationCounts, len(rows))
	for _, row := range rows {
		out[fromPgID[domain.EventID](row.EventID)] = store.EventRegistrationCounts{
			Registered: int(row.Registered), Invited: int(row.Invited), Waiting: int(row.Waiting),
		}
	}

	return out, nil
}

func (r eventRegistrationRepo) AnonymizeGuests(ctx context.Context, endedBefore time.Time) (int, error) {
	n, err := r.q.AnonymizeGuestEventRegistrations(ctx, sqlc.AnonymizeGuestEventRegistrationsParams{
		GuestName: pgText(domain.AnonymousGuestName), EndedBefore: pgInstant(endedBefore),
	})

	return int(n), mapErr(err)
}
