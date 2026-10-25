package postgres

import (
	"context"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// ---------------------------------------------------------------- push subscriptions

type pushSubscriptionRepo struct{ q *sqlc.Queries }

func toPushSubscription(row sqlc.PushSubscription) domain.PushSubscription {
	return domain.PushSubscription{
		ID:          fromPgID[domain.PushSubscriptionID](row.ID),
		AccountID:   fromPgID[domain.AccountID](row.AccountID),
		Endpoint:    row.Endpoint,
		P256DH:      [65]byte(row.P256dh),
		Auth:        [16]byte(row.Auth),
		DeviceLabel: row.DeviceLabel.String,
		CreatedAt:   fromPgInstant(row.CreatedAt),
		LastUsedAt:  fromPgInstant(row.LastUsedAt),
	}
}

func (r pushSubscriptionRepo) Get(ctx context.Context, id domain.PushSubscriptionID) (domain.PushSubscription, error) {
	row, err := r.q.GetPushSubscription(ctx, pgID(id))
	if err != nil {
		return domain.PushSubscription{}, notFound("push subscription", err)
	}

	return toPushSubscription(row), nil
}

func (r pushSubscriptionRepo) Save(ctx context.Context, s domain.PushSubscription) error {
	if _, err := r.q.DeleteMovedPushSubscription(ctx, sqlc.DeleteMovedPushSubscriptionParams{
		Endpoint: s.Endpoint, AccountID: pgID(s.AccountID),
	}); err != nil {
		return mapErr(err)
	}

	return mapErr(r.q.SavePushSubscription(ctx, sqlc.SavePushSubscriptionParams{
		ID:          pgID(s.ID),
		AccountID:   pgID(s.AccountID),
		Endpoint:    s.Endpoint,
		P256dh:      s.P256DH[:],
		Auth:        s.Auth[:],
		DeviceLabel: pgText(s.DeviceLabel),
		CreatedAt:   pgInstant(s.CreatedAt),
		LastUsedAt:  pgInstant(s.LastUsedAt),
	}))
}

func (r pushSubscriptionRepo) ListByAccount(
	ctx context.Context, account domain.AccountID,
) ([]domain.PushSubscription, error) {
	rows, err := r.q.ListPushSubscriptionsByAccount(ctx, pgID(account))

	return convertRows(rows, err, toPushSubscription)
}

func (r pushSubscriptionRepo) Delete(ctx context.Context, id domain.PushSubscriptionID) error {
	n, err := r.q.DeletePushSubscription(ctx, pgID(id))

	return affected("push subscription", n, err)
}

func (r pushSubscriptionRepo) DeleteByAccount(ctx context.Context, account domain.AccountID) (int, error) {
	n, err := r.q.DeletePushSubscriptionsByAccount(ctx, pgID(account))

	return int(n), mapErr(err)
}

func (r pushSubscriptionRepo) DeleteByEndpoint(
	ctx context.Context, account domain.AccountID, endpoint string,
) (int, error) {
	n, err := r.q.DeletePushSubscriptionByEndpoint(ctx, sqlc.DeletePushSubscriptionByEndpointParams{
		AccountID: pgID(account), Endpoint: endpoint,
	})

	return int(n), mapErr(err)
}

func (r pushSubscriptionRepo) Touch(ctx context.Context, id domain.PushSubscriptionID, at time.Time) error {
	n, err := r.q.TouchPushSubscription(ctx, sqlc.TouchPushSubscriptionParams{LastUsedAt: pgInstant(at), ID: pgID(id)})

	return affected("push subscription", n, err)
}

func (r pushSubscriptionRepo) DeleteUnusedBefore(ctx context.Context, before time.Time) (int, error) {
	n, err := r.q.DeleteUnusedPushSubscriptions(ctx, pgAt(before))

	return int(n), mapErr(err)
}

// ---------------------------------------------------------------- push outbox

type pushOutboxRepo struct{ q *sqlc.Queries }

func toPushMessage(row sqlc.PushOutbox) domain.PushMessage {
	return domain.PushMessage{
		ID:             fromPgID[domain.PushMessageID](row.ID),
		SubscriptionID: fromPgID[domain.PushSubscriptionID](row.SubscriptionID),
		Payload:        string(row.Payload),
		CreatedAt:      fromPgInstant(row.CreatedAt),
		ExpiresAt:      fromPgInstant(row.ExpiresAt),
		Attempts:       int(row.Attempts),
		NextAttemptAt:  fromPgInstant(row.NextAttemptAt),
		SentAt:         fromPgInstant(row.SentAt),
		FailedAt:       fromPgInstant(row.FailedAt),
		LastError:      row.LastError.String,
	}
}

func (r pushOutboxRepo) Get(ctx context.Context, id domain.PushMessageID) (domain.PushMessage, error) {
	row, err := r.q.GetPushMessage(ctx, pgID(id))
	if err != nil {
		return domain.PushMessage{}, notFound("push message", err)
	}

	return toPushMessage(row), nil
}

func (r pushOutboxRepo) Create(ctx context.Context, m domain.PushMessage) error {
	attempts, err := int4(m.Attempts)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreatePushMessage(ctx, sqlc.CreatePushMessageParams{
		ID:             pgID(m.ID),
		SubscriptionID: pgID(m.SubscriptionID),
		Payload:        []byte(m.Payload),
		CreatedAt:      pgInstant(m.CreatedAt),
		ExpiresAt:      pgInstant(m.ExpiresAt),
		Attempts:       attempts,
		NextAttemptAt:  pgInstant(m.NextAttemptAt),
		SentAt:         pgInstant(m.SentAt),
		FailedAt:       pgInstant(m.FailedAt),
		LastError:      pgText(m.LastError),
	}))
}

func (r pushOutboxRepo) Update(ctx context.Context, m domain.PushMessage) error {
	attempts, err := int4(m.Attempts)
	if err != nil {
		return err
	}

	n, err := r.q.UpdatePushMessage(ctx, sqlc.UpdatePushMessageParams{
		ID:            pgID(m.ID),
		Payload:       []byte(m.Payload),
		ExpiresAt:     pgInstant(m.ExpiresAt),
		Attempts:      attempts,
		NextAttemptAt: pgInstant(m.NextAttemptAt),
		SentAt:        pgInstant(m.SentAt),
		FailedAt:      pgInstant(m.FailedAt),
		LastError:     pgText(m.LastError),
	})

	return affected("push message", n, err)
}

func (r pushOutboxRepo) ListDue(ctx context.Context, now time.Time, limit int) ([]domain.PushMessage, error) {
	rows, err := r.q.ListDuePushMessages(ctx, sqlc.ListDuePushMessagesParams{Now: pgInstant(now), MaxRows: rowLimit(limit)})

	return convertRows(rows, err, toPushMessage)
}

func (r pushOutboxRepo) DeleteCreatedBefore(ctx context.Context, before time.Time) (int, error) {
	n, err := r.q.DeleteOldPushMessages(ctx, pgAt(before))

	return int(n), mapErr(err)
}
